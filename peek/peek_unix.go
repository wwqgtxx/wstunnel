//go:build !windows

package peek

import (
	"io"
	"net"
	"syscall"
)

func NewPeekConn(conn net.Conn) Conn {
	rc, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		return NewBufferedConn(conn)
	}
	if pc, ok := conn.(*peekConn); ok {
		return pc
	}
	return &peekConn{
		Conn: conn,
		rc:   rc,
	}
}

type peekConn struct {
	net.Conn
	rc syscall.RawConn
}

func (c *peekConn) Peek(n int) ([]byte, error) {
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
