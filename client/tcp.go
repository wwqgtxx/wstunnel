package client

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/proxy"
	"github.com/wwqgtxx/wstunnel/tunnel"
	"github.com/wwqgtxx/wstunnel/utils"
)

type tcpClientImpl struct {
	targetAddress string
	dialer        proxy.ContextDialer
	proxy         string
}

func (c *tcpClientImpl) Target() string {
	return c.targetAddress
}

func (c *tcpClientImpl) Proxy() string {
	return c.proxy
}

func (c *tcpClientImpl) Handle(tcp net.Conn) {
	defer tcp.Close()
	log.Println("Incoming --> ", tcp.RemoteAddr(), " --> ", c.Target(), c.Proxy())
	conn, err := c.Dial(nil, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	conn.TunnelTcp(tcp)
}

func (c *tcpClientImpl) Dial(edBuf []byte, inHeader http.Header) (common.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DialTimeout)
	defer cancel()
	tcp, err := c.dialer.DialContext(ctx, "tcp", c.Target())
	if err == nil && len(edBuf) > 0 {
		_, err = tcp.Write(edBuf)
		if err != nil {
			return nil, err
		}
	}
	return &tcpClientConn{tcp: tcp}, err
}

type tcpClientConn struct {
	tcp   net.Conn
	close sync.Once
}

func (c *tcpClientConn) Close() {
	c.close.Do(func() {
		_ = c.tcp.Close()
	})
}

func (c *tcpClientConn) TunnelTcp(tcp net.Conn) {
	tunnel.Tunnel(tcp, c.tcp)
}

func (c *tcpClientConn) TunnelWs(wsConn *utils.WebsocketConn) {
	tunnel.Tunnel(c.tcp, wsConn)
}

func NewTcpClientImpl(clientConfig config.ClientConfig) (common.ClientImpl, error) {
	dialer, proxyStr := proxy.FromProxyString(clientConfig.Proxy)

	return &tcpClientImpl{
		targetAddress: clientConfig.TargetAddress,
		dialer:        dialer,
		proxy:         proxyStr,
	}, nil
}
