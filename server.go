package main

import (
	"log"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  BufSize,
	WriteBufferSize: BufSize,
	WriteBufferPool: WriteBufferPool,
}

type Server interface {
	Handler(w http.ResponseWriter, r *http.Request)
}

type normalServer struct {
	DestAddress string
}

func (s *normalServer) Handler(w http.ResponseWriter, r *http.Request) {
	log.Println("[Server]Incoming --> ", r.RemoteAddr, r.Header, s.DestAddress)

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

type internalServer struct {
	DestAddress string
	Client      Client
}

func (s *internalServer) Handler(w http.ResponseWriter, r *http.Request) {
	log.Println("[Server]Incoming --> ", r.RemoteAddr, r.Header, " --> ( [Client]", s.DestAddress, ") --> ", s.Client.Target())

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()
	source := ws.UnderlyingConn()

	ws2, err := s.Client.Dial()
	if err != nil {
		log.Println(err)
		return
	}
	defer ws2.Close()
	target := s.Client.ToRawConn(ws2)

	TunnelTcpTcp(target, source)
}

func StartServer(config ServerConfig) {
	mux := http.NewServeMux()
	for _, target := range config.Target {
		if len(target.WSPath) == 0 {
			target.WSPath = "/"
		}
		host, port, err := net.SplitHostPort(target.TargetAddress)
		if err != nil {
			log.Println(err)
		}
		var s Server
		client, ok := PortToClient[port]
		if ok && (host == "127.0.0.1" || host == "localhost") {
			s = &internalServer{
				DestAddress: target.TargetAddress,
				Client:      client,
			}
		} else {
			s = &normalServer{
				DestAddress: target.TargetAddress,
			}

		}
		mux.HandleFunc(target.WSPath, s.Handler)
	}
	go log.Print(http.ListenAndServe(config.BindAddress, mux))
}
