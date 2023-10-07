package common

import (
	"net"
	"net/http"

	"github.com/wwqgtxx/wstunnel/utils"
)

var PortToServer = make(map[string]Server)

type Server interface {
	HasListenerConfig
	Start()
	Addr() string
	CloneWithNewAddress(bindAddress string) Server
}

var PortToClient = make(map[string]Client)

type Client interface {
	ClientImpl
	HasListenerConfig
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
	Dial(edBuf []byte, inHeader http.Header) (ClientConn, error)
}

type ClientConn interface {
	Close()
	TunnelTcp(tcp net.Conn)
	TunnelWs(wsConn *utils.WebsocketConn)
}

type HasListenerConfig interface {
	GetListenerConfig() any
	SetListenerConfig(cfg any)
}
