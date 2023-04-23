package peekws

// modify from https://github.com/MetaCubeX/Clash.Meta/blob/4a0d097fe9f1b8fe352267040658331168e8abd8/transport/vmess/websocket.go

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/wwqgtxx/wstunnel/peek"
	"github.com/wwqgtxx/wstunnel/peek/deadline"

	"github.com/gorilla/websocket"
)

type websocketConn struct {
	conn       *websocket.Conn
	reader     io.Reader
	remoteAddr net.Addr

	// https://godoc.org/github.com/gorilla/websocket#hdr-Concurrency
	rMux sync.Mutex
	wMux sync.Mutex
}

// Read implements net.Conn.Read()
func (wsc *websocketConn) Read(b []byte) (int, error) {
	wsc.rMux.Lock()
	defer wsc.rMux.Unlock()
	for {
		reader, err := wsc.getReader()
		if err != nil {
			return 0, err
		}

		nBytes, err := reader.Read(b)
		if err == io.EOF {
			wsc.reader = nil
			continue
		}
		return nBytes, err
	}
}

// Write implements io.Writer.
func (wsc *websocketConn) Write(b []byte) (int, error) {
	wsc.wMux.Lock()
	defer wsc.wMux.Unlock()
	if err := wsc.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (wsc *websocketConn) Close() error {
	var e []string
	if err := wsc.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second*5)); err != nil {
		e = append(e, err.Error())
	}
	if err := wsc.conn.Close(); err != nil {
		e = append(e, err.Error())
	}
	if len(e) > 0 {
		return fmt.Errorf("failed to close connection: %s", strings.Join(e, ","))
	}
	return nil
}

func (wsc *websocketConn) getReader() (io.Reader, error) {
	if wsc.reader != nil {
		return wsc.reader, nil
	}

	_, reader, err := wsc.conn.NextReader()
	if err != nil {
		return nil, err
	}
	wsc.reader = reader
	return reader, nil
}

func (wsc *websocketConn) LocalAddr() net.Addr {
	return wsc.conn.LocalAddr()
}

func (wsc *websocketConn) RemoteAddr() net.Addr {
	return wsc.remoteAddr
}

func (wsc *websocketConn) SetDeadline(t time.Time) error {
	if err := wsc.SetReadDeadline(t); err != nil {
		return err
	}
	return wsc.SetWriteDeadline(t)
}

func (wsc *websocketConn) SetReadDeadline(t time.Time) error {
	return wsc.conn.SetReadDeadline(t)
}

func (wsc *websocketConn) SetWriteDeadline(t time.Time) error {
	return wsc.conn.SetWriteDeadline(t)
}

type edPeekConn struct {
	peek.Conn
	edBuf []byte
}

func (c *edPeekConn) Peek(n int) ([]byte, error) {
	edBufLen := len(c.edBuf)
	if n <= edBufLen {
		return c.edBuf[:n], nil
	}
	if edBufLen > 0 {
		bb, err := c.Conn.Peek(n - edBufLen)
		if err != nil {
			return nil, err
		}
		b := make([]byte, n)
		nn := copy(b, c.edBuf)
		copy(b[nn:], bb)
		return b, err
	}
	return c.Conn.Peek(n)
}

func (c *edPeekConn) ReaderReplaceable() bool {
	return true
}

func (c *edPeekConn) ToReader() io.Reader {
	return c.Conn
}

func (c *edPeekConn) WriterReplaceable() bool {
	return true
}

func (c *edPeekConn) ToWriter() io.Writer {
	return c.Conn
}

func New(ws *websocket.Conn, rAddr net.Addr, edBuf []byte) (c peek.Conn) {
	// websocketConn can't correct handle ReadDeadline
	// gorilla/websocket will cache the os.ErrDeadlineExceeded from conn.Read()
	// it will cause read fail and event panic in *websocket.Conn.NextReader()
	// so call deadline.New to add a safe wrapper
	c = peek.NewBufferedConn(deadline.New(&websocketConn{
		conn:       ws,
		remoteAddr: rAddr,
	}))
	if len(edBuf) > 0 {
		c = &edPeekConn{c, edBuf}
	}
	return
}
