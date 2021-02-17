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
	listener, err := net.Listen("tcp", config.BindAddress)
	if err != nil {
		log.Println(err)
	}
	header := http.Header{}
	if len(config.WSHeaders) != 0 {
		for key, value := range config.WSHeaders {
			header.Add(key, value)
		}
	}
	wsDialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
		ReadBufferSize:   BufSize,
		WriteBufferSize:  BufSize,
		WriteBufferPool:  WriteBufferPool,
	}
	wsDialer.TLSClientConfig = &tls.Config{
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.SkipCertVerify,
	}
	tcpDialer := net.Dialer{
		Timeout: 45 * time.Second,
	}
	for {
		tcp, err := listener.Accept()
		if err != nil {
			log.Println(err)
			<-time.After(3 * time.Second)
			continue
		}
		go func() {
			defer tcp.Close()
			if len(config.TargetAddress) > 0 {
				conn, err := tcpDialer.Dial("tcp", config.TargetAddress)
				if err != nil {
					log.Println(err)
					return
				}
				defer conn.Close()

				TunnelTcpTcp(tcp, conn)

			} else {
				ws, _, err := wsDialer.Dial(config.WSUrl, header)
				if err != nil {
					log.Println(err)
					return
				}
				defer ws.Close()

				TunnelTcpWs(tcp, ws)
			}

		}()
	}
}
