package peek

import (
	"bufio"
	"io"
	"net"
)

type BufferedConn struct {
	r *bufio.Reader
	net.Conn
}

func NewBufferedConn(c net.Conn) *BufferedConn {
	if bc, ok := c.(*BufferedConn); ok {
		return bc
	}
	return &BufferedConn{bufio.NewReader(c), c}
}

// Reader returns the internal bufio.Reader.
func (c *BufferedConn) Reader() *bufio.Reader {
	return c.r
}

// Peek returns the next n bytes without advancing the reader.
func (c *BufferedConn) Peek(n int) ([]byte, error) {
	return c.r.Peek(n)
}

func (c *BufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *BufferedConn) ReadByte() (byte, error) {
	return c.r.ReadByte()
}

func (c *BufferedConn) UnreadByte() error {
	return c.r.UnreadByte()
}

func (c *BufferedConn) Buffered() int {
	return c.r.Buffered()
}

func (c *BufferedConn) WriteTo(w io.Writer) (n int64, err error) {
	return c.r.WriteTo(w)
}

func (c *BufferedConn) ReaderReplaceable() bool {
	if c.r.Buffered() > 0 {
		return false
	}
	return true
}

func (c *BufferedConn) ToReader() io.Reader {
	if c.r.Buffered() > 0 {
		return c
	}
	return c.Conn
}

func (c *BufferedConn) WriterReplaceable() bool {
	return true
}

func (c *BufferedConn) ToWriter() io.Writer {
	return c.Conn
}
