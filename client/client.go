package client

import (
	"crypto/tls"
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
	"github.com/wwqgtxx/wstunnel/listener"
	"github.com/wwqgtxx/wstunnel/tunnel"
	"github.com/wwqgtxx/wstunnel/utils"

	"github.com/gorilla/websocket"
)

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
	header   http.Header
	wsUrl    string
	wsDialer *websocket.Dialer
	ed       uint32
	proxy    string
}

type tcpClientImpl struct {
	targetAddress string
	netDial       NetDialerFunc
	proxy         string
}

func (c *wsClientImpl) Target() string {
	return c.wsUrl
}

func (c *wsClientImpl) Proxy() string {
	return c.proxy
}

func (c *wsClientImpl) Handle(tcp net.Conn) {
	defer tcp.Close()
	log.Println("Incoming --> ", tcp.RemoteAddr(), " --> ", c.Target(), c.Proxy())
	header, edBuf, err := utils.EncodeXray0rtt(tcp, c.ed)
	if err != nil {
		log.Println(err)
		return
	}
	conn, err := c.Dial(edBuf, header)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	conn.TunnelTcp(tcp)
}

func (c *wsClientImpl) Dial(edBuf []byte, inHeader http.Header) (common.ClientConn, error) {
	header := c.header
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
	}
	if c.ed > 0 && len(edBuf) > 0 {
		header.Set("Sec-WebSocket-Protocol", utils.EncodeEd(edBuf))
		edBuf = nil
	}
	log.Println("Dial to", c.Target(), c.Proxy(), "with", header)
	ws, resp, err := c.wsDialer.Dial(c.Target(), header)
	if resp != nil {
		log.Println("Dial", c.Target(), c.Proxy(), "get response", resp.Header)
	}
	if len(edBuf) > 0 {
		err = ws.WriteMessage(websocket.BinaryMessage, edBuf)
		if err != nil {
			return nil, err
		}
	}
	return &wsClientConn{ws: ws}, err
}

type wsClientConn struct {
	ws    *websocket.Conn
	close sync.Once
}

func (c *wsClientConn) Close() {
	c.close.Do(func() {
		_ = c.ws.Close()
	})
}

func (c *wsClientConn) TunnelTcp(tcp net.Conn) {
	tunnel.TcpWs(tcp, c.ws)
}

func (c *wsClientConn) TunnelWs(ws *websocket.Conn) {
	// fastpath for direct tunnel underlying ws connection
	tunnel.TcpTcp(ws.UnderlyingConn(), c.ws.UnderlyingConn())
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
	tcp, err := c.netDial("tcp", c.Target())
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
	tunnel.TcpTcp(tcp, c.tcp)
}

func (c *tcpClientConn) TunnelWs(ws *websocket.Conn) {
	tunnel.TcpWs(c.tcp, ws)
}

func BuildClient(clientConfig config.ClientConfig) {
	_, port, err := net.SplitHostPort(clientConfig.BindAddress)
	if err != nil {
		log.Println(err)
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

func NewClientImpl(clientConfig config.ClientConfig) common.ClientImpl {
	if len(clientConfig.TargetAddress) > 0 {
		return NewTcpClientImpl(clientConfig)
	} else {
		return NewWsClientImpl(clientConfig)
	}
}

func NewTcpClientImpl(clientConfig config.ClientConfig) common.ClientImpl {
	proxyUrl, proxyStr := parseProxy(clientConfig.Proxy)

	var netDial NetDialerFunc
	tcpDialer := &net.Dialer{
		Timeout: 8 * time.Second,
	}
	netDial = tcpDialer.Dial

	proxyDialer := proxy_FromEnvironment()
	if proxyUrl != nil {
		dialer, err := proxy_FromURL(proxyUrl, netDial)
		if err != nil {
			log.Println(err)
		} else {
			proxyDialer = dialer
		}
	}
	if proxyDialer != proxy_Direct {
		netDial = proxyDialer.Dial
	}

	return &tcpClientImpl{
		targetAddress: clientConfig.TargetAddress,
		netDial:       netDial,
		proxy:         proxyStr,
	}
}

func NewWsClientImpl(clientConfig config.ClientConfig) common.ClientImpl {
	proxyUrl, proxyStr := parseProxy(clientConfig.Proxy)

	proxy := http.ProxyFromEnvironment
	if proxyUrl != nil {
		proxy = http.ProxyURL(proxyUrl)
	}

	header := http.Header{}
	if len(clientConfig.WSHeaders) != 0 {
		for key, value := range clientConfig.WSHeaders {
			header.Add(key, value)
		}
	}
	wsDialer := &websocket.Dialer{
		Proxy:            proxy,
		HandshakeTimeout: 8 * time.Second,
		ReadBufferSize:   tunnel.BufSize,
		WriteBufferSize:  tunnel.BufSize,
		WriteBufferPool:  tunnel.WriteBufferPool,
	}
	wsDialer.TLSClientConfig = &tls.Config{
		ServerName:         clientConfig.ServerName,
		InsecureSkipVerify: clientConfig.SkipCertVerify,
	}
	var ed uint32
	if u, err := url.Parse(clientConfig.WSUrl); err == nil {
		if q := u.Query(); q.Get("ed") != "" {
			Ed, _ := strconv.Atoi(q.Get("ed"))
			ed = uint32(Ed)
			q.Del("ed")
			u.RawQuery = q.Encode()
			clientConfig.WSUrl = u.String()
		}
	}
	return &wsClientImpl{
		header:   header,
		wsUrl:    clientConfig.WSUrl,
		wsDialer: wsDialer,
		ed:       ed,
		proxy:    proxyStr,
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
	listener.NewClientImpl = NewClientImpl
}
