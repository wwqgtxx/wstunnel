package main

import (
	"crypto/tls"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

var PortToClient = make(map[string]Client)

type Client interface {
	ClientImpl
	Start()
	Handle(tcp net.Conn)
	Addr() string
	GetClientImpl() ClientImpl
	SetClientImpl(impl ClientImpl)
}

type client struct {
	ClientImpl
	bindAddress string
}

func (c *client) Start() {
	log.Println("New Client Listening on:", c.bindAddress)
	go func() {
		listener, err := net.Listen("tcp", c.bindAddress)
		if err != nil {
			log.Println(err)
			return
		}
		for {
			tcp, err := listener.Accept()
			if err != nil {
				log.Println(err)
				<-time.After(3 * time.Second)
				continue
			}
			go c.Handle(tcp)
		}
	}()
}

func (c *client) Handle(tcp net.Conn) {
	defer tcp.Close()
	log.Println("Incoming --> ", tcp.RemoteAddr(), " --> ", c.Target())
	conn, err := c.Dial()
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	c.Tunnel(tcp, conn)
}

func (c *client) Addr() string {
	return c.bindAddress
}

func (c *client) GetClientImpl() ClientImpl {
	return c.ClientImpl
}
func (c *client) SetClientImpl(impl ClientImpl) {
	c.ClientImpl = impl
}

type ClientImpl interface {
	Target() string
	Dial(args ...interface{}) (io.Closer, error)
	ToRawConn(conn io.Closer) net.Conn
	Tunnel(tcp net.Conn, conn io.Closer)
}

type wsClientImpl struct {
	header   http.Header
	wsUrl    string
	wsDialer *websocket.Dialer
}

type tcpClientImpl struct {
	targetAddress string
	tcpDialer     *net.Dialer
}

func (c *wsClientImpl) Target() string {
	return c.wsUrl
}

func (c *wsClientImpl) Dial(args ...interface{}) (io.Closer, error) {
	header := c.header
	if len(args) >= 1 {
		if inHeader, ok := args[0].(http.Header); ok {
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
	}
	log.Println("Dial to", c.Target(), "with", header)
	ws, _, err := c.wsDialer.Dial(c.Target(), header)
	return ws, err
}

func (c *wsClientImpl) ToRawConn(conn io.Closer) net.Conn {
	ws := conn.(*websocket.Conn)
	return ws.UnderlyingConn()
}

func (c *wsClientImpl) Tunnel(tcp net.Conn, conn io.Closer) {
	TunnelTcpWs(tcp, conn.(*websocket.Conn))
}

func (c *tcpClientImpl) Target() string {
	return c.targetAddress
}

func (c *tcpClientImpl) Dial(args ...interface{}) (io.Closer, error) {
	return c.tcpDialer.Dial("tcp", c.Target())
}

func (c *tcpClientImpl) ToRawConn(conn io.Closer) net.Conn {
	return conn.(net.Conn)
}

func (c *tcpClientImpl) Tunnel(tcp net.Conn, conn io.Closer) {
	TunnelTcpTcp(tcp, conn.(net.Conn))
}

func BuildClient(config ClientConfig) {
	var c Client
	if len(config.TargetAddress) > 0 {
		tcpDialer := &net.Dialer{
			Timeout: 45 * time.Second,
		}
		c = &client{
			ClientImpl: &tcpClientImpl{
				targetAddress: config.TargetAddress,
				tcpDialer:     tcpDialer,
			},
			bindAddress: config.BindAddress,
		}
	} else {
		header := http.Header{}
		if len(config.WSHeaders) != 0 {
			for key, value := range config.WSHeaders {
				header.Add(key, value)
			}
		}
		wsDialer := &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 45 * time.Second,
			ReadBufferSize:   BufSize,
			WriteBufferSize:  BufSize,
			WriteBufferPool:  WriteBufferPool,
		}
		wsDialer.TLSClientConfig = &tls.Config{
			ServerName:         config.ServerName,
			InsecureSkipVerify: config.SkipCertVerify,
		}
		c = &client{
			ClientImpl: &wsClientImpl{
				header:   header,
				wsUrl:    config.WSUrl,
				wsDialer: wsDialer,
			},
			bindAddress: config.BindAddress,
		}
	}

	_, port, err := net.SplitHostPort(config.BindAddress)
	if err != nil {
		log.Println(err)
	}
	PortToClient[port] = c
}

func StartClients() {
	for clientPort, client := range PortToClient {
		if !strings.HasPrefix(client.Target(), "ws") {
			host, port, err := net.SplitHostPort(client.Target())
			if err != nil {
				log.Println(err)
			}

			if host == "127.0.0.1" || host == "localhost" {
				if _server, ok := PortToServer[port]; ok {
					log.Println("Short circuit replace (",
						client.Addr(), "<->", client.Target(),
						") to ( [Server]",
						client.Addr(),
						")")
					server := _server.CloneWithNewAddress(client.Addr())
					PortToServer[clientPort] = server
					delete(PortToClient, clientPort) //It is safe in Golang!!!
					continue
				}

				if _client, ok := PortToClient[port]; ok {
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
