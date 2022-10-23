//go:build !windows

package peek

import (
	"io"
	"net"
	"syscall"
)

func Peek(conn net.Conn, n int) (net.Conn, []byte, error) {
	var sysErr error = nil
	rc, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		return conn, nil, err
	}
	buf := make([]byte, n)
	err = rc.Read(func(fd uintptr) bool {
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
		return conn, nil, err
	}
	return conn, buf, sysErr
}
