package client

import (
	"context"
	"log"
	"net"
	"net/http"

	"github.com/wwqgtxx/wstunnel/client/mtproxy/tools"
	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/proxy"
	"github.com/wwqgtxx/wstunnel/tunnel"
	"github.com/wwqgtxx/wstunnel/utils"
)

type mtproxyClientImpl struct {
	serverInfo *tools.ServerInfo
	dialer     proxy.ContextDialer
	proxyStr   string
}

var _ common.ClientImpl = (*mtproxyClientImpl)(nil)

func (c *mtproxyClientImpl) Target() string {
	return "TELEGRAM"
}

func (c *mtproxyClientImpl) Proxy() string {
	return c.proxyStr
}

func (c *mtproxyClientImpl) SNI() string {
	return c.serverInfo.CloakHost
}

func (c *mtproxyClientImpl) Handle(tcp net.Conn) {
	serverProtocol := c.serverInfo.ServerProtocolMaker(
		c.serverInfo.Secret,
		c.serverInfo.SecretMode,
		c.serverInfo.CloakHost,
		c.serverInfo.CloakPort,
	)
	serverConn, err := serverProtocol.Handshake(tcp)
	if err != nil {
		//log.Warnln("Cannot perform mtproxyClientImpl handshake: %s", err)

		return
	}
	defer serverConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), DialTimeout)
	defer cancel()

	telegramConn, err := c.serverInfo.TelegramDialer.Dial(
		serverProtocol,
		func(addr string) (net.Conn, error) {
			return c.dialer.DialContext(ctx, "tcp", addr)
		})
	if err != nil {
		return
	}
	defer telegramConn.Close()

	log.Println("Tunnel MTP From", tcp.RemoteAddr(), " --> ", telegramConn.RemoteAddr())

	tunnel.Tunnel(serverConn, telegramConn)
}

func (c *mtproxyClientImpl) Dial(edBuf []byte, inHeader http.Header) (common.ClientConn, error) {
	return &mtproxyClientConn{mtproxyClientImpl: c, edBuf: edBuf}, nil
}

type mtproxyClientConn struct {
	*mtproxyClientImpl
	edBuf []byte
}

func (c *mtproxyClientConn) Close() {}

func (c *mtproxyClientConn) TunnelTcp(tcp net.Conn) {
	if len(c.edBuf) > 0 {
		tcp = utils.NewCachedConn(tcp, c.edBuf)
	}
	c.Handle(tcp)
}

func (c *mtproxyClientConn) TunnelWs(wsConn *utils.WebsocketConn) {
	c.TunnelTcp(wsConn)
}

var _ common.ClientConn = (*mtproxyClientConn)(nil)

func NewMtproxyClientImpl(clientConfig config.ClientConfig) (common.ClientImpl, error) {
	serverInfo, err := tools.ParseHexedSecret(clientConfig.Mtp)
	if err != nil {
		return nil, err
	}
	dialer, proxyStr := proxy.FromProxyString(clientConfig.Proxy)
	return &mtproxyClientImpl{serverInfo: serverInfo, dialer: dialer, proxyStr: proxyStr}, nil
}
