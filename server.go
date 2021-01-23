package main

import (
	"log"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Server struct {
	DestAddress string
}

func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	log.Println("Incoming --> ", r.RemoteAddr, r.Header)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	tcp, err := net.Dial("tcp", s.DestAddress)
	if err != nil {
		log.Println(err)
		return
	}
	defer tcp.Close()

	TunnelTcpWs(tcp, conn)
}

func server(config ServerConfig) {
	mux := http.NewServeMux()
	for _, target := range config.Target {
		s := Server{
			DestAddress: target.TargetAddress,
		}
		if len(target.WSPath) == 0 {
			target.WSPath = "/"
		}
		mux.HandleFunc(target.WSPath, s.handler)
	}
	log.Print(http.ListenAndServe(config.BindAddress, mux))
}
