package peekws

// modify from https://github.com/MetaCubeX/Clash.Meta/blob/4a0d097fe9f1b8fe352267040658331168e8abd8/transport/vmess/websocket.go

import (
	"io"
	"net"
	"time"

	"github.com/wwqgtxx/wstunnel/peek"
	"github.com/wwqgtxx/wstunnel/peek/deadline"
	"github.com/wwqgtxx/wstunnel/utils"

	"github.com/gorilla/websocket"
)

type websocketConn struct {
	io.Reader
	io.Writer
	io.Closer
	conn       *websocket.Conn
	remoteAddr net.Addr
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
		Reader:     utils.NewWsReader(ws),
		Writer:     utils.NewWsWriter(ws),
		Closer:     utils.NewWsCloser(ws),
		conn:       ws,
		remoteAddr: rAddr,
	}))
	if len(edBuf) > 0 {
		c = &edPeekConn{c, edBuf}
	}
	return
}
