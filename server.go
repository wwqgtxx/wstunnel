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
	go func() {
		buf := make([]byte, 1024)
		for {
			len, err := tcp.Read(buf)
			if err != nil {
				log.Println(err)
				conn.Close()
				tcp.Close()
				break
			}
			conn.WriteMessage(websocket.BinaryMessage, buf[0:len])
		}
	}()
	for {
		msgType, buf, err := conn.ReadMessage()
		if err != nil {
			log.Println(err)
			conn.Close()
			tcp.Close()
			break
		}
		if msgType != websocket.BinaryMessage {
			log.Println("unknown msgType")
		}
		tcp.Write(buf)
	}
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
	log.Fatal(http.ListenAndServe(config.BindAddress, mux))
}
