//go:build windows

package peek

import (
	"net"
)

func NewPeekConn(conn net.Conn) Conn {
	return NewBufferedConn(conn)
}
