package server

import (
	"io"
	"log"
	"net"
	"net/http"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/listener"
	"github.com/wwqgtxx/wstunnel/tunnel"
	"github.com/wwqgtxx/wstunnel/utils"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  tunnel.BufSize,
	WriteBufferSize: tunnel.BufSize,
	WriteBufferPool: tunnel.WriteBufferPool,
}

type server struct {
	serverHandler  ServerHandler
	listenerConfig config.ListenerConfig
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

type ServerHandler http.Handler

type normalServerHandler struct {
	DestAddress string
}

func (s *normalServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		closeTcpHandle(w, r)
		return
	}

	log.Println("Incoming --> ", r.RemoteAddr, r.Header, s.DestAddress)

	ch := make(chan net.Conn)
	defer func() {
		if i, ok := <-ch; ok {
			_ = i.Close()
		}
	}()

	edBuf, responseHeader := utils.DecodeXray0rtt(r.Header)

	go func() {
		defer close(ch)
		tcp, err := net.Dial("tcp", s.DestAddress)
		if err != nil {
			log.Println(err)
			return
		}

		if len(edBuf) > 0 {
			_, err = tcp.Write(edBuf)
			if err != nil {
				log.Println(err)
				return
			}
		}
		ch <- tcp
	}()

	ws, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()

	tcp, ok := <-ch
	if !ok {
		return
	}
	defer tcp.Close()

	tunnel.TunnelTcpWs(tcp, ws)
}

type internalServerHandler struct {
	DestAddress string
	Proxy       string
	Client      common.Client
}

func (s *internalServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		closeTcpHandle(w, r)
		return
	}

	log.Println("Incoming --> ", r.RemoteAddr, r.Header, " --> ( [Client]", s.DestAddress, s.Proxy, ") --> ", s.Client.Target())

	ch := make(chan io.Closer)
	defer func() {
		if i, ok := <-ch; ok {
			_ = i.Close()
		}
	}()

	edBuf, responseHeader := utils.DecodeXray0rtt(r.Header)

	go func() {
		defer close(ch)
		// send inHeader to client for Xray's 0rtt ws
		ws2, err := s.Client.Dial(edBuf, r.Header)
		if err != nil {
			log.Println(err)
			return
		}
		ch <- ws2
	}()

	ws, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()
	source := ws.UnderlyingConn()

	ws2, ok := <-ch
	if !ok {
		return
	}
	defer ws2.Close()
	target := s.Client.ToRawConn(ws2)

	tunnel.TunnelTcpTcp(target, source)
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
		}
		var sh ServerHandler
		_client, ok := common.PortToClient[port]
		if ok && (host == "127.0.0.1" || host == "localhost") {
			log.Println("Short circuit replace (",
				target.WSPath, "<->", target.TargetAddress,
				") to (",
				target.WSPath, "<->", _client.Target(), _client.Proxy(),
				")")
			sh = &internalServerHandler{
				DestAddress: target.TargetAddress,
				Proxy:       _client.Proxy(),
				Client:      _client,
			}
		} else {
			sh = &normalServerHandler{
				DestAddress: target.TargetAddress,
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
		serverHandler:  mux,
		listenerConfig: serverConfig.ListenerConfig,
	}
	_, port, err := net.SplitHostPort(serverConfig.BindAddress)
	if err != nil {
		log.Println(err)
	}
	common.PortToServer[port] = s
}

func StartServers() {
	for _, server := range common.PortToServer {
		server.Start()
	}
}
