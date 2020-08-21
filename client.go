package main

import (
	"crypto/tls"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
	"time"
)

func client(config ClientConfig) {
	header := http.Header{}
	if len(config.WSHeaders) != 0 {
		for key, value := range config.WSHeaders {
			header.Add(key, value)
		}
	}
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}
	dialer.TLSClientConfig = &tls.Config{
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.SkipCertVerify,
	}
	listener, err := net.Listen("tcp", config.BindAddress)
	if err != nil {
		log.Println(err)
	}
	for {
		tcp, err := listener.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		go func() {
			defer tcp.Close()
			ws, _, err := dialer.Dial(config.WSUrl, header)
			if err != nil {
				log.Println(err)
				return
			}
			defer ws.Close()

			Tunnel(tcp, ws)
		}()
	}
}
