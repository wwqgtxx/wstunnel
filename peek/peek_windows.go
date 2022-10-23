//go:build windows

package peek

import (
	"net"
)

func Peek(conn net.Conn, n int) (net.Conn, []byte, error) {
	bufConn := NewBufferedConn(conn)
	data, err := bufConn.Peek(n)
	return bufConn, data, err
}
