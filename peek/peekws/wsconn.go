package peekws

import (
	"github.com/wwqgtxx/wstunnel/peek"
	"github.com/wwqgtxx/wstunnel/peek/deadline"
	"github.com/wwqgtxx/wstunnel/utils"
	"io"
)

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

func New(wsConn *utils.WebsocketConn, edBuf []byte) (c peek.Conn) {
	// websocketConn can't correct handle ReadDeadline
	// so call deadline.New to add a safe wrapper
	c = peek.NewBufferedConn(deadline.New(wsConn))
	if len(edBuf) > 0 {
		c = &edPeekConn{c, edBuf}
	}
	return
}
