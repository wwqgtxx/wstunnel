package common

import (
	"io"
	"net"
	"net/http"

	"github.com/wwqgtxx/wstunnel/config"
)

var PortToServer = make(map[string]Server)

type Server interface {
	Start()
	Addr() string
	CloneWithNewAddress(bindAddress string) Server
}

var PortToClient = make(map[string]Client)

type Client interface {
	ClientImpl
	Start()
	Addr() string
	GetClientImpl() ClientImpl
	SetClientImpl(impl ClientImpl)
	GetServerWSPath() string
}

type ClientImpl interface {
	Target() string
	Proxy() string
	Handle(tcp net.Conn)
	Dial(edBuf []byte, inHeader http.Header) (io.Closer, error)
	ToRawConn(conn io.Closer) net.Conn
	Tunnel(tcp net.Conn, conn io.Closer)
}

var NewClientImpl func(config config.ClientConfig) ClientImpl
