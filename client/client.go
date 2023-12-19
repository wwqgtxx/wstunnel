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
	"strings"
	"sync"
	"time"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback"
	"github.com/wwqgtxx/wstunnel/listener"
	"github.com/wwqgtxx/wstunnel/proxy"
	"github.com/wwqgtxx/wstunnel/tunnel"
	"github.com/wwqgtxx/wstunnel/utils"
)

const DialTimeout = 8 * time.Second

type client struct {
	common.ClientImpl
	serverWSPath   string
	listenerConfig listener.Config
}

func (c *client) Start() {
	log.Println("New Client Listening on:", c.Addr())
	go func() {
		ln, err := listener.ListenTcp(c.listenerConfig)
		if err != nil {
			log.Println(err)
			return
		}
		for {
			tcp, err := ln.Accept()
			if err != nil {
				log.Println(err)
				<-time.After(3 * time.Second)
				continue
			}
			go c.Handle(tcp)
		}
	}()
}

func (c *client) Addr() string {
	return c.listenerConfig.BindAddress
}

func (c *client) GetClientImpl() common.ClientImpl {
	return c.ClientImpl
}

func (c *client) SetClientImpl(impl common.ClientImpl) {
	c.ClientImpl = impl
}

func (c *client) GetListenerConfig() any {
	return c.listenerConfig
}

func (c *client) SetListenerConfig(cfg any) {
	c.listenerConfig = cfg.(listener.Config)
	c.listenerConfig.IsWebSocketListener = false
}

func (c *client) GetServerWSPath() string {
	return c.serverWSPath
}

type wsClientImpl struct {
	header           http.Header
	wsUrl            *url.URL
	tlsConfig        *tls.Config
	dialer           proxy.ContextDialer
	ed               uint32
	proxy            string
	v2rayHttpUpgrade bool
}

type tcpClientImpl struct {
	targetAddress string
	dialer        proxy.ContextDialer
	proxy         string
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

func BuildClient(clientConfig config.ClientConfig) {
	_, port, err := net.SplitHostPort(clientConfig.BindAddress)
	if err != nil {
		log.Println(err)
		return
	}

	serverWSPath := strings.ReplaceAll(clientConfig.ServerWSPath, "{port}", port)

	c := &client{
		ClientImpl:   NewClientImpl(clientConfig),
		serverWSPath: serverWSPath,
		listenerConfig: listener.Config{
			ListenerConfig:      clientConfig.ListenerConfig,
			ProxyConfig:         clientConfig.ProxyConfig,
			IsWebSocketListener: len(clientConfig.TargetAddress) > 0,
		},
	}

	common.PortToClient[port] = c
}

func parseProxy(proxyString string) (proxyUrl *url.URL, proxyStr string) {
	if len(proxyString) > 0 {
		u, err := url.Parse(proxyString)
		if err != nil {
			log.Println(err)
		}
		proxyUrl = u

		ru := *u
		ru.User = nil
		proxyStr = ru.String()
	}
	return
}

func getDialer(proxyUrl *url.URL) proxy.ContextDialer {
	tcpDialer := &net.Dialer{}

	proxyDialer := proxy.FromEnvironment()
	if proxyUrl != nil {
		dialer, err := proxy.FromURL(proxyUrl, tcpDialer)
		if err != nil {
			log.Println(err)
		} else {
			proxyDialer = dialer
		}
	}
	if proxyDialer != proxy.Direct {
		return proxy.NewContextDialer(proxyDialer)
	} else {
		return tcpDialer
	}
}

func NewClientImpl(clientConfig config.ClientConfig) common.ClientImpl {
	if len(clientConfig.TargetAddress) > 0 {
		return NewTcpClientImpl(clientConfig)
	} else {
		return NewWsClientImpl(clientConfig)
	}
}

func NewTcpClientImpl(clientConfig config.ClientConfig) common.ClientImpl {
	proxyUrl, proxyStr := parseProxy(clientConfig.Proxy)
	dialer := getDialer(proxyUrl)

	return &tcpClientImpl{
		targetAddress: clientConfig.TargetAddress,
		dialer:        dialer,
		proxy:         proxyStr,
	}
}

func NewWsClientImpl(clientConfig config.ClientConfig) common.ClientImpl {
	proxyUrl, proxyStr := parseProxy(clientConfig.Proxy)
	netDial := getDialer(proxyUrl)

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
		dialer:           netDial,
		tlsConfig:        tlsConfig,
		ed:               ed,
		proxy:            proxyStr,
		v2rayHttpUpgrade: clientConfig.V2rayHttpUpgrade,
	}
}

func StartClients() {
	for clientPort, client := range common.PortToClient {
		if !strings.HasPrefix(client.Target(), "ws") {
			host, port, err := net.SplitHostPort(client.Target())
			if err != nil {
				log.Println(err)
			}

			if host == "127.0.0.1" || host == "localhost" {
				if _server, ok := common.PortToServer[port]; ok {
					log.Println("Short circuit replace (",
						client.Addr(), "<->", client.Target(),
						") to ( [Server]",
						client.Addr(),
						")")
					newServer := _server.CloneWithNewAddress(client.Addr())
					listenerConfig := client.GetListenerConfig()
					newServer.SetListenerConfig(listenerConfig)
					common.PortToServer[clientPort] = newServer
					delete(common.PortToClient, clientPort) //It is safe in Golang!!!
					continue
				}

				if _client, ok := common.PortToClient[port]; ok {
					log.Println("Short circuit replace (",
						client.Addr(), "<->", client.Target(),
						") to (",
						client.Addr(), "<->", _client.Target(),
						")")
					client.SetClientImpl(_client.GetClientImpl())
				}
			}
		}
		client.Start()
	}
}

func init() {
	fallback.NewClientImpl = NewClientImpl
}
