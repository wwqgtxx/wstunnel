package tunnel

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/wwqgtxx/wstunnel/peek"

	"github.com/gorilla/websocket"
)

const (
	BufSize = 4096
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

	exit := make(chan struct{}, 1)

	go func() {
		err := fromTcpToWs(tcp, ws)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		_ = ws.SetReadDeadline(time.Now())
		exit <- struct{}{}
	}()

	err := fromWsToTcp(ws, tcp)
	if err != nil && err == io.EOF {
		log.Println(err)
	}
	_ = tcp.SetReadDeadline(time.Now())

	<-exit
}

func TcpTcp(tcp1 net.Conn, tcp2 net.Conn) {
	setKeepAlive(tcp1)
	setKeepAlive(tcp2)

	exit := make(chan struct{}, 1)

	go func() {
		_, err := io.Copy(peek.ToWriter(tcp1), peek.ToReader(tcp2))
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		_ = tcp1.SetReadDeadline(time.Now())
		exit <- struct{}{}
	}()

	_, err := io.Copy(peek.ToWriter(tcp2), peek.ToReader(tcp1))
	if err != nil && err == io.EOF {
		log.Println(err)
	}
	_ = tcp2.SetReadDeadline(time.Now())

	<-exit
}

func setKeepAlive(c net.Conn) {
	writer := peek.ToWriter(c) // writer is always the underlying conn in peek.Conn
	if conn, ok := writer.(interface{ SetKeepAlive(keepalive bool) error }); ok {
		_ = conn.SetKeepAlive(true)
	}
	if conn, ok := writer.(interface{ SetKeepAlivePeriod(d time.Duration) error }); ok {
		_ = conn.SetKeepAlivePeriod(30 * time.Second)
	}
}
