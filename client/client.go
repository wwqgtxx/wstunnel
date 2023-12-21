package client

import (
	"log"
	"net"
	"strings"
	"time"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback"
	"github.com/wwqgtxx/wstunnel/listener"
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

func BuildClient(clientConfig config.ClientConfig) {
	_, port, err := net.SplitHostPort(clientConfig.BindAddress)
	if err != nil {
		log.Println(err)
		return
	}

	serverWSPath := strings.ReplaceAll(clientConfig.ServerWSPath, "{port}", port)

	clientImpl, err := NewClientImpl(clientConfig)
	if err != nil {
		log.Println(err)
		return
	}

	c := &client{
		ClientImpl:   clientImpl,
		serverWSPath: serverWSPath,
		listenerConfig: listener.Config{
			ListenerConfig:      clientConfig.ListenerConfig,
			ProxyConfig:         clientConfig.ProxyConfig,
			IsWebSocketListener: len(clientConfig.TargetAddress) > 0,
		},
	}

	common.PortToClient[port] = c
}

func NewClientImpl(clientConfig config.ClientConfig) (common.ClientImpl, error) {
	switch {
	case len(clientConfig.Mtp) > 0:
		return NewMtproxyClientImpl(clientConfig)
	case len(clientConfig.TargetAddress) > 0:
		return NewTcpClientImpl(clientConfig)
	default:
		return NewWsClientImpl(clientConfig)
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
