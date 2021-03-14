package main

import (
	"log"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

var PortToServer = make(map[string]Server)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  BufSize,
	WriteBufferSize: BufSize,
	WriteBufferPool: WriteBufferPool,
}

type Server interface {
	Start()
	Addr() string
	CloneWithNewAddress(bindAddress string) Server
}

type server struct {
	bindAddress   string
	serverHandler ServerHandler
}

func (s *server) Start() {
	log.Println("New Server Listening on:", s.bindAddress)
	go func() {
		err := http.ListenAndServe(s.bindAddress, s.serverHandler)
		if err != nil {
			log.Println(err)
		}
	}()
}

func (c *server) Addr() string {
	return c.bindAddress
}

func (s *server) CloneWithNewAddress(bindAddress string) Server {
	return &server{
		bindAddress:   bindAddress,
		serverHandler: s.serverHandler,
	}
}

type ServerHandler http.Handler

type normalServerHandler struct {
	DestAddress string
}

func (s *normalServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("Incoming --> ", r.RemoteAddr, r.Header, s.DestAddress)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()

	tcp, err := net.Dial("tcp", s.DestAddress)
	if err != nil {
		log.Println(err)
		return
	}
	defer tcp.Close()

	TunnelTcpWs(tcp, ws)
}

type internalServerHandler struct {
	DestAddress string
	Client      Client
}

func (s *internalServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("Incoming --> ", r.RemoteAddr, r.Header, " --> ( [Client]", s.DestAddress, ") --> ", s.Client.Target())

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()
	source := ws.UnderlyingConn()

	ws2, err := s.Client.Dial(r.Header)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws2.Close()
	target := s.Client.ToRawConn(ws2)

	TunnelTcpTcp(target, source)
}

func BuildServer(config ServerConfig) {
	mux := http.NewServeMux()
	for _, target := range config.Target {
		if len(target.WSPath) == 0 {
			target.WSPath = "/"
		}
		host, port, err := net.SplitHostPort(target.TargetAddress)
		if err != nil {
			log.Println(err)
		}
		var sh ServerHandler
		client, ok := PortToClient[port]
		if ok && (host == "127.0.0.1" || host == "localhost") {
			log.Println("Short circuit replace (",
				target.WSPath, "<->", target.TargetAddress,
				") to (",
				target.WSPath, "<->", client.Target(),
				")")
			sh = &internalServerHandler{
				DestAddress: target.TargetAddress,
				Client:      client,
			}
		} else {
			sh = &normalServerHandler{
				DestAddress: target.TargetAddress,
			}

		}
		mux.Handle(target.WSPath, sh)
	}
	var s Server
	s = &server{
		bindAddress:   config.BindAddress,
		serverHandler: mux,
	}
	_, port, err := net.SplitHostPort(config.BindAddress)
	if err != nil {
		log.Println(err)
	}
	PortToServer[port] = s
}

func StartServers() {
	for _, server := range PortToServer {
		server.Start()
	}
}
