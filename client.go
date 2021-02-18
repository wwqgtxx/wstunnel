package main

import (
	"crypto/tls"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

var PortToClient = make(map[string]Client)

type Client interface {
	Target() string
	Dial() (io.Closer, error)
	ToRawConn(conn io.Closer) net.Conn
	Handle(tcp net.Conn)
}

type wsClient struct {
	header   http.Header
	wsUrl    string
	wsDialer *websocket.Dialer
}

type tcpClient struct {
	targetAddress string
	tcpDialer     *net.Dialer
}

func (c *wsClient) Target() string {
	return c.wsUrl
}

func (c *wsClient) Dial() (io.Closer, error) {
	ws, _, err := c.wsDialer.Dial(c.Target(), c.header)
	return ws, err
}

func (c *wsClient) ToRawConn(conn io.Closer) net.Conn {
	ws := conn.(*websocket.Conn)
	return ws.UnderlyingConn()
}

func (c *wsClient) Handle(tcp net.Conn) {
	defer tcp.Close()
	log.Println("Incoming --> ", tcp.RemoteAddr(), " --> ", c.Target())
	conn, err := c.Dial()
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	TunnelTcpWs(tcp, conn.(*websocket.Conn))
}

func (c *tcpClient) Target() string {
	return c.targetAddress
}

func (c *tcpClient) Dial() (io.Closer, error) {
	return c.tcpDialer.Dial("tcp", c.Target())
}

func (c *tcpClient) ToRawConn(conn io.Closer) net.Conn {
	return conn.(net.Conn)
}

func (c *tcpClient) Handle(tcp net.Conn) {
	defer tcp.Close()
	log.Println("Incoming --> ", tcp.RemoteAddr(), " --> ", c.Target())
	conn, err := c.Dial()
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	TunnelTcpTcp(tcp, conn.(net.Conn))
}

func StartClient(config ClientConfig) {
	var client Client
	if len(config.TargetAddress) > 0 {
		tcpDialer := &net.Dialer{
			Timeout: 45 * time.Second,
		}
		client = &tcpClient{
			targetAddress: config.TargetAddress,
			tcpDialer:     tcpDialer,
		}

		host, port, err := net.SplitHostPort(config.TargetAddress)
		if err != nil {
			log.Println(err)
		}
		_client, ok := PortToClient[port]
		if ok && (host == "127.0.0.1" || host == "localhost") {
			log.Println("Short circuit replace (",
				config.BindAddress, "<->", client.Target(),
				") to (",
				config.BindAddress, "<->", _client.Target(),
				")")
			client = _client
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
		client = &wsClient{
			header:   header,
			wsUrl:    config.WSUrl,
			wsDialer: wsDialer,
		}
	}

	_, port, err := net.SplitHostPort(config.BindAddress)
	if err != nil {
		log.Println(err)
	}
	PortToClient[port] = client

	go func() {
		listener, err := net.Listen("tcp", config.BindAddress)
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
			go client.Handle(tcp)
		}
	}()
}
