package client

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	listenerConfig config.ListenerConfig
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
	c.Tunnel(tcp, conn)
}

func (c *wsClientImpl) Dial(edBuf []byte, inHeader http.Header) (io.Closer, error) {
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
			header.Set("Sec-WebSocket-Protocol", secProtocol)
		}
	}
	log.Println("Dial to", c.Target(), c.Proxy(), "with", header)
	ws, resp, err := c.wsDialer.Dial(c.Target(), header)
	if resp != nil {
		log.Println("Dial", c.Target(), c.Proxy(), "get response", resp.Header)
	}
	return ws, err
}

func (c *wsClientImpl) ToRawConn(conn io.Closer) net.Conn {
	ws := conn.(*websocket.Conn)
	return ws.UnderlyingConn()
}

func (c *wsClientImpl) Tunnel(tcp net.Conn, conn io.Closer) {
	tunnel.TunnelTcpWs(tcp, conn.(*websocket.Conn))
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
	c.Tunnel(tcp, conn)
}

func (c *tcpClientImpl) Dial(edBuf []byte, inHeader http.Header) (io.Closer, error) {
	tcp, err := c.netDial("tcp", c.Target())
	if err == nil && len(edBuf) > 0 {
		_, err = tcp.Write(edBuf)
	}
	return tcp, err
}

func (c *tcpClientImpl) ToRawConn(conn io.Closer) net.Conn {
	return conn.(net.Conn)
}

func (c *tcpClientImpl) Tunnel(tcp net.Conn, conn io.Closer) {
	tunnel.TunnelTcpTcp(tcp, conn.(net.Conn))
}

func BuildClient(clientConfig config.ClientConfig) {
	_, port, err := net.SplitHostPort(clientConfig.BindAddress)
	if err != nil {
		log.Println(err)
	}

	serverWSPath := strings.ReplaceAll(clientConfig.ServerWSPath, "{port}", port)

	c := &client{
		ClientImpl:     NewClientImpl(clientConfig),
		serverWSPath:   serverWSPath,
		listenerConfig: clientConfig.ListenerConfig,
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

func NewClientImpl(config config.ClientConfig) common.ClientImpl {
	if len(config.TargetAddress) > 0 {
		return NewTcpClientImpl(config)
	} else {
		return NewWsClientImpl(config)
	}
}

func NewTcpClientImpl(config config.ClientConfig) common.ClientImpl {
	proxyUrl, proxyStr := parseProxy(config.Proxy)

	var netDial NetDialerFunc
	tcpDialer := &net.Dialer{
		Timeout: 45 * time.Second,
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
		targetAddress: config.TargetAddress,
		netDial:       netDial,
		proxy:         proxyStr,
	}
}

func NewWsClientImpl(config config.ClientConfig) common.ClientImpl {
	proxyUrl, proxyStr := parseProxy(config.Proxy)

	proxy := http.ProxyFromEnvironment
	if proxyUrl != nil {
		proxy = http.ProxyURL(proxyUrl)
	}

	header := http.Header{}
	if len(config.WSHeaders) != 0 {
		for key, value := range config.WSHeaders {
			header.Add(key, value)
		}
	}
	wsDialer := &websocket.Dialer{
		Proxy:            proxy,
		HandshakeTimeout: 45 * time.Second,
		ReadBufferSize:   tunnel.BufSize,
		WriteBufferSize:  tunnel.BufSize,
		WriteBufferPool:  tunnel.WriteBufferPool,
	}
	wsDialer.TLSClientConfig = &tls.Config{
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.SkipCertVerify,
	}
	var ed uint32
	if u, err := url.Parse(config.WSUrl); err == nil {
		if q := u.Query(); q.Get("ed") != "" {
			Ed, _ := strconv.Atoi(q.Get("ed"))
			ed = uint32(Ed)
			q.Del("ed")
			u.RawQuery = q.Encode()
			config.WSUrl = u.String()
		}
	}
	return &wsClientImpl{
		header:   header,
		wsUrl:    config.WSUrl,
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
	common.NewClientImpl = NewClientImpl
}
