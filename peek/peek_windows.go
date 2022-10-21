//go:build windows

package peek

import (
	"net"
)

func Peek(conn net.Conn, buf []byte) (net.Conn, error) {
	bufConn := NewBufferedConn(conn)
	data, err := bufConn.Peek(len(buf))
	if err != nil {
		return bufConn, err
	}
	copy(buf, data)
	return bufConn, nil
}
