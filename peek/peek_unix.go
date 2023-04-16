//go:build !windows

package peek

import (
	"io"
	"net"
	"syscall"
)

func NewPeekConn(conn net.Conn) Conn {
	if pc, ok := conn.(Conn); ok {
		return pc
	}
	rc, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		return NewBufferedConn(conn)
	}
	return &peekConn{
		Conn: conn,
		peek: peek{rc},
	}
}

type peek struct {
	rc syscall.RawConn
}

func (c *peek) Peek(n int) ([]byte, error) {
	var sysErr error = nil
	buf := make([]byte, n)
	err := c.rc.Read(func(fd uintptr) bool {
		n, _, err := syscall.Recvfrom(int(fd), buf, syscall.MSG_PEEK)
		switch {
		case n == 0 && err == nil:
			sysErr = io.EOF
		case err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR:
			return false
			//sysErr = nil
		default:
			sysErr = err
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return buf, sysErr
}

type peekConn struct {
	net.Conn
	peek
}

func (c *peekConn) ReaderReplaceable() bool {
	return true
}

func (c *peekConn) ToReader() io.Reader {
	return c.Conn
}

func (c *peekConn) WriterReplaceable() bool {
	return true
}

func (c *peekConn) ToWriter() io.Writer {
	return c.Conn
}
