package main

import (
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net"
	"sync"
)

const (
	BufSize = 1024
)

var (
	BufPool         = sync.Pool{New: func() interface{} { return make([]byte, BufSize) }}
	WriteBufferPool = &sync.Pool{}
)

func TcpToWs(tcp net.Conn, ws *websocket.Conn) (err error) {
	buf := BufPool.Get().([]byte)
	for {
		nBytes, err := tcp.Read(buf)
		if err != nil {
			break
		}
		err = ws.WriteMessage(websocket.BinaryMessage, buf[0:nBytes])
		if err != nil {
			break
		}
	}
	BufPool.Put(buf)
	return
}

func WsToTcp(ws *websocket.Conn, tcp net.Conn) (err error) {
	var reader io.Reader

	buf := BufPool.Get().([]byte)
	for {
		if reader == nil {
			var msgType int
			msgType, reader, err = ws.NextReader()
			if err != nil {
				break
			}
			if msgType != websocket.BinaryMessage {
				log.Println("unknown msgType")
			}
		}
		// _, err := io.CopyBuffer(tcp,reader,buf)
		nBytes, err := reader.Read(buf)
		if err == io.EOF {
			reader = nil
			err = nil
			continue
		}
		if err != nil {
			break
		}
		nBytes, err = tcp.Write(buf[0:nBytes])
		if err != nil {
			break
		}
	}
	BufPool.Put(buf)
	return
}

func TunnelTcpWs(tcp net.Conn, ws *websocket.Conn) {
	exit := make(chan int, 2)

	go func() {
		err := TcpToWs(tcp, ws)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		exit <- 1
	}()

	go func() {
		err := WsToTcp(ws, tcp)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		exit <- 1
	}()
	<-exit
}

func TunnelTcpTcp(tcp1 net.Conn, tcp2 net.Conn) {
	exit := make(chan int, 2)

	go func() {
		_, err := io.Copy(tcp1, tcp2)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		exit <- 1
	}()

	go func() {
		_, err := io.Copy(tcp2, tcp1)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		exit <- 1
	}()
	<-exit
}
