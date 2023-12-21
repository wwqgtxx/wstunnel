package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/proxy"
	"github.com/wwqgtxx/wstunnel/tunnel"
	"github.com/wwqgtxx/wstunnel/utils"
)

type wsClientImpl struct {
	header           http.Header
	wsUrl            *url.URL
	tlsConfig        *tls.Config
	dialer           proxy.ContextDialer
	ed               uint32
	proxy            string
	v2rayHttpUpgrade bool
}

func (c *wsClientImpl) Target() string {
	return c.wsUrl.String()
}

func (c *wsClientImpl) Proxy() string {
	return c.proxy
}

func (c *wsClientImpl) Handle(tcp net.Conn) {
	defer tcp.Close()
	log.Println("Incoming --> ", tcp.RemoteAddr(), " --> ", c.Target(), c.Proxy())
	edBuf, err := utils.PrepareXray0rtt(tcp, c.ed)
	if err != nil {
		log.Println(err)
		return
	}
	conn, err := c.Dial(edBuf, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	conn.TunnelTcp(tcp)
}

func (c *wsClientImpl) Dial(edBuf []byte, inHeader http.Header) (common.ClientConn, error) {
	var header http.Header
	if len(inHeader) > 0 {
		// copy from inHeader
		header = inHeader.Clone()
		// don't use inHeader's `Host`
		header.Del("Host")

		// merge from c.header
		for k, vs := range c.header {
			header[k] = vs
		}

		// duplicate header is not allowed, remove
		header.Del("Upgrade")
		header.Del("Connection")
		header.Del("Sec-Websocket-Key")
		header.Del("Sec-Websocket-Version")
		header.Del("Sec-Websocket-Extensions")
		header.Del("Sec-WebSocket-Protocol")

		// force use inHeader's `Sec-WebSocket-Protocol` for Xray's 0rtt ws
		if secProtocol := inHeader.Get("Sec-WebSocket-Protocol"); len(secProtocol) > 0 {
			if c.ed > 0 {
				header.Set("Sec-WebSocket-Protocol", secProtocol)
				edBuf = nil
			} else {
				edBuf, _ = utils.DecodeEd(secProtocol)
			}
		}
	} else {
		// copy from c.header
		header = c.header.Clone()
		if header == nil {
			header = http.Header{}
		}
	}
	if c.ed > 0 && len(edBuf) > 0 {
		header.Set("Sec-WebSocket-Protocol", utils.EncodeEd(edBuf))
		edBuf = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), DialTimeout)
	defer cancel()
	conn, respHeader, err := utils.ClientWebsocketDial(ctx, *c.wsUrl, header, c.dialer, c.tlsConfig, c.v2rayHttpUpgrade)
	log.Println("Dial to", c.Target(), c.Proxy(), "with", header, "response", respHeader)
	if err != nil {
		return nil, err
	}

	if len(edBuf) > 0 {
		_, err = conn.Write(edBuf)
		if err != nil {
			return nil, err
		}
	}
	if wsConn, ok := conn.(*utils.WebsocketConn); ok {
		return &wsClientConn{wsConn: wsConn}, err
	} else {
		return &tcpClientConn{tcp: conn}, err
	}
}

type wsClientConn struct {
	wsConn *utils.WebsocketConn
	close  sync.Once
}

func (c *wsClientConn) Close() {
	c.close.Do(func() {
		_ = c.wsConn.Close()
	})
}

func (c *wsClientConn) TunnelTcp(tcp net.Conn) {
	tunnel.Tunnel(tcp, c.wsConn)
}

func (c *wsClientConn) TunnelWs(wsConn *utils.WebsocketConn) {
	if wsConn.ReaderReplaceable() == c.wsConn.ReaderReplaceable() {
		// fastpath for direct tunnel underlying ws connection
		tunnel.Tunnel(wsConn.Conn, c.wsConn.Conn)
	} else {
		tunnel.Tunnel(wsConn, c.wsConn)
	}
}

func NewWsClientImpl(clientConfig config.ClientConfig) (common.ClientImpl, error) {
	dialer, proxyStr := proxy.FromProxyString(clientConfig.Proxy)

	header := http.Header{}
	if len(clientConfig.WSHeaders) != 0 {
		for key, value := range clientConfig.WSHeaders {
			header.Add(key, value)
		}
	}
	tlsConfig := &tls.Config{
		ServerName:         clientConfig.ServerName,
		InsecureSkipVerify: clientConfig.SkipCertVerify,
	}
	var ed uint32
	u, err := url.Parse(clientConfig.WSUrl)
	if err != nil {
		panic(fmt.Errorf("parse url %s error: %w", clientConfig.WSUrl, err))
	}
	if q := u.Query(); q.Get("ed") != "" {
		Ed, _ := strconv.Atoi(q.Get("ed"))
		ed = uint32(Ed)
		q.Del("ed")
		u.RawQuery = q.Encode()
		//clientConfig.WSUrl = u.String()
	}

	return &wsClientImpl{
		header:           header,
		wsUrl:            u,
		dialer:           dialer,
		tlsConfig:        tlsConfig,
		ed:               ed,
		proxy:            proxyStr,
		v2rayHttpUpgrade: clientConfig.V2rayHttpUpgrade,
	}, nil
}
