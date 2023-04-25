package utils

import (
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type wsWriter struct {
	ws *websocket.Conn
}

func NewWsWriter(ws *websocket.Conn) io.Writer {
	return wsWriter{ws}
}

func (w wsWriter) Write(p []byte) (n int, err error) {
	err = w.ws.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return
	}
	n = len(p)
	return
}

type wsReader struct {
	ws     *websocket.Conn
	reader io.Reader
}

func NewWsReader(ws *websocket.Conn) io.Reader {
	return &wsReader{ws, nil}
}

func (r *wsReader) Read(p []byte) (n int, err error) {
	for {
		if r.reader == nil {
			var msgType int
			msgType, r.reader, err = r.ws.NextReader()
			if err != nil {
				return
			}
			if msgType != websocket.BinaryMessage && msgType != websocket.TextMessage {
				log.Println("unknown msgType")
			}
		}
		n, err = r.reader.Read(p)
		if err == io.EOF {
			r.reader = nil
			continue
		}
		return
	}
}

type wsCloser struct {
	ws *websocket.Conn
}

func NewWsCloser(ws *websocket.Conn) io.Closer {
	return wsCloser{ws}
}

func (c wsCloser) Close() error {
	var e []string
	if err := c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second*5)); err != nil {
		e = append(e, err.Error())
	}
	if err := c.ws.Close(); err != nil {
		e = append(e, err.Error())
	}
	if len(e) > 0 {
		return fmt.Errorf("failed to close connection: %s", strings.Join(e, ","))
	}
	return nil
}
