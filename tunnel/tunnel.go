package tunnel

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	BufSize = 1024
)

var (
	BufPool         = sync.Pool{New: func() any { return make([]byte, BufSize) }}
	WriteBufferPool = &sync.Pool{}
)

func fromTcpToWs(tcp net.Conn, ws *websocket.Conn) (err error) {
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

func fromWsToTcp(ws *websocket.Conn, tcp net.Conn) (err error) {
	var reader io.Reader

	buf := BufPool.Get().([]byte)
	for {
		if reader == nil {
			var msgType int
			msgType, reader, err = ws.NextReader()
			if err != nil {
				break
			}
			if msgType != websocket.BinaryMessage && msgType != websocket.TextMessage {
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

func TcpWs(tcp net.Conn, ws *websocket.Conn) {
	setKeepAlive(tcp)
	setKeepAlive(ws.UnderlyingConn())

	exit := make(chan int, 2)

	go func() {
		err := fromTcpToWs(tcp, ws)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		exit <- 1
	}()

	go func() {
		err := fromWsToTcp(ws, tcp)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		exit <- 1
	}()
	<-exit
}

func TcpTcp(tcp1 net.Conn, tcp2 net.Conn) {
	setKeepAlive(tcp1)
	setKeepAlive(tcp2)

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

func setKeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
}
