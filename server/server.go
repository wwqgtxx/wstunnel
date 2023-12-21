package server

import (
	"log"
	"net"
	"net/http"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback"
	"github.com/wwqgtxx/wstunnel/listener"
	"github.com/wwqgtxx/wstunnel/peek/peekws"
	"github.com/wwqgtxx/wstunnel/utils"
)

type server struct {
	serverHandler  ServerHandler
	listenerConfig listener.Config
}

func (s *server) Start() {
	log.Println("New Server Listening on:", s.Addr())
	go func() {
		server := &http.Server{Addr: s.Addr(), Handler: s.serverHandler}
		ln, err := listener.ListenTcp(s.listenerConfig)
		if err != nil {
			log.Println(err)
			return
		}
		err = server.Serve(ln)
		if err != nil {
			log.Println(err)
			return
		}
	}()
}

func (s *server) Addr() string {
	return s.listenerConfig.BindAddress
}

func (s *server) CloneWithNewAddress(bindAddress string) common.Server {
	ns := *s
	ns.listenerConfig.BindAddress = bindAddress
	return &ns
}

func (s *server) GetListenerConfig() any {
	return s.listenerConfig
}

func (s *server) SetListenerConfig(cfg any) {
	s.listenerConfig = cfg.(listener.Config)
	s.listenerConfig.IsWebSocketListener = true
}

type ServerHandler http.Handler

type serverHandler struct {
	common.ClientImpl
	DestAddress string
	Fallback    *fallback.Fallback
	IsInternal  bool
}

func (s *serverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !utils.IsWebSocketUpgrade(r) {
		closeTcpHandle(w, r)
		return
	}

	if s.IsInternal {
		log.Println("Incoming --> ", r.RemoteAddr, r.Header, " --> ( [Client]", s.DestAddress, s.Proxy(), ") --> ", s.Target())
	} else {
		if len(s.Proxy()) > 0 {
			log.Println("Incoming --> ", r.RemoteAddr, r.Header, " --> ( ", s.Proxy(), ") --> ", s.Target())
		} else {
			log.Println("Incoming --> ", r.RemoteAddr, r.Header, s.Target())
		}
	}

	edBuf := utils.DecodeXray0rtt(r.Header)

	if s.Fallback != nil {
		wsConn, err := utils.ServerWebsocketUpgrade(w, r)
		if err != nil {
			log.Println(err)
			return
		}
		defer wsConn.Close()
		conn := peekws.New(wsConn, edBuf)
		if s.Fallback.Handle(conn, edBuf, r.Header) {
			return
		}
		// send inHeader to client for Xray's 0rtt ws
		target, err := s.Dial(edBuf, r.Header)
		if err != nil {
			log.Println(err)
			return
		}
		defer target.Close()
		target.TunnelTcp(conn)
		return
	}

	ch := make(chan common.ClientConn)
	defer func() {
		if i, ok := <-ch; ok {
			i.Close()
		}
	}()

	go func() {
		defer close(ch)
		// send inHeader to client for Xray's 0rtt ws
		target, err := s.Dial(edBuf, r.Header)
		if err != nil {
			log.Println(err)
			return
		}
		ch <- target
	}()

	wsConn, err := utils.ServerWebsocketUpgrade(w, r)
	if err != nil {
		log.Println(err)
		return
	}
	defer wsConn.Close()

	target, ok := <-ch
	if !ok {
		return
	}
	defer target.Close()
	target.TunnelWs(wsConn)
}

func closeTcpHandle(writer http.ResponseWriter, request *http.Request) {
	h, ok := writer.(http.Hijacker)
	if !ok {
		return
	}
	netConn, _, err := h.Hijack()
	if err != nil {
		return
	}
	_ = netConn.Close()
}

func BuildServer(serverConfig config.ServerConfig) {
	mux := http.NewServeMux()
	hadRoot := false
	for port, _client := range common.PortToClient {
		wsPath := _client.GetServerWSPath()
		if len(wsPath) > 0 {
			serverConfig.Target = append(serverConfig.Target, config.ServerTargetConfig{
				WSPath:        wsPath,
				TargetAddress: net.JoinHostPort("127.0.0.1", port),
			})
		}
	}
	for _, target := range serverConfig.Target {
		if len(target.WSPath) == 0 {
			target.WSPath = "/"
		}
		host, port, err := net.SplitHostPort(target.TargetAddress)
		if err != nil {
			log.Println(err)
			continue
		}
		var sh ServerHandler
		_client, ok := common.PortToClient[port]
		if ok && (host == "127.0.0.1" || host == "localhost") {
			log.Println("Short circuit replace (",
				target.WSPath, "<->", target.TargetAddress,
				") to (",
				target.WSPath, "<->", _client.Target(), _client.Proxy(),
				")")
			listenerConfig := _client.GetListenerConfig().(listener.Config)
			fb, _ := fallback.NewFallback(fallback.Config{
				FallbackConfig:      listenerConfig.FallbackConfig,
				ProxyConfig:         listenerConfig.ProxyConfig,
				IsWebSocketListener: false,
			})
			sh = &serverHandler{
				ClientImpl:  _client.GetClientImpl(),
				DestAddress: target.TargetAddress,
				IsInternal:  true,
				Fallback:    fb,
			}
		} else {
			proxyConfig := serverConfig.ProxyConfig
			if target.ProxyConfig != nil {
				proxyConfig = *target.ProxyConfig
			}
			clientImpl, err := fallback.NewClientImpl(config.ClientConfig{TargetAddress: target.TargetAddress, ProxyConfig: proxyConfig})
			if err != nil {
				log.Println(err)
				continue
			}
			sh = &serverHandler{
				ClientImpl:  clientImpl,
				DestAddress: target.TargetAddress,
				IsInternal:  false,
			}
		}
		if target.WSPath == "/" {
			hadRoot = true
		}
		mux.Handle(target.WSPath, sh)
	}
	if !hadRoot {
		mux.HandleFunc("/", closeTcpHandle)
	}
	var s common.Server
	s = &server{
		serverHandler: mux,
		listenerConfig: listener.Config{
			ListenerConfig:      serverConfig.ListenerConfig,
			ProxyConfig:         serverConfig.ProxyConfig,
			IsWebSocketListener: true,
		},
	}
	_, port, err := net.SplitHostPort(serverConfig.BindAddress)
	if err != nil {
		log.Println(err)
		return
	}
	common.PortToServer[port] = s
}

func StartServers() {
	for _, server := range common.PortToServer {
		server.Start()
	}
}
