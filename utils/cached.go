package utils

import (
	"io"
	"net"
)

type CachedConn struct {
	net.Conn
	data []byte
}

func NewCachedConn(c net.Conn, data []byte) *CachedConn {
	return &CachedConn{c, data}
}

func (c *CachedConn) Read(b []byte) (n int, err error) {
	if len(c.data) > 0 {
		n = copy(b, c.data)
		c.data = c.data[n:]
		return
	}
	return c.Conn.Read(b)
}

func (c *CachedConn) ReaderReplaceable() bool {
	if len(c.data) > 0 {
		return false
	}
	return true
}

func (c *CachedConn) ToReader() io.Reader {
	if len(c.data) > 0 {
		return c
	}
	return c.Conn
}

func (c *CachedConn) WriterReplaceable() bool {
	return true
}

func (c *CachedConn) ToWriter() io.Writer {
	return c.Conn
}
