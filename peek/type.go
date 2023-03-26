package peek

import "net"

type Conn interface {
	net.Conn
	Peeker
}

type Peeker interface {
	Peek(n int) ([]byte, error)
}
