//go:build windows

package udp

import (
	"net"
	"net/netip"
)

type enhanceUDPConn struct {
	*net.UDPConn
}

func (c *enhanceUDPConn) WaitReadFrom() (data []byte, put func(), addr netip.AddrPort, err error) {
	readBuf := BufPool.Get().([]byte)
	put = func() {
		BufPool.Put(readBuf)
	}
	var readN int
	readN, addr, err = c.UDPConn.ReadFromUDPAddrPort(readBuf)
	if readN > 0 {
		data = readBuf[:readN]
	} else {
		put()
		put = nil
	}
	return
}
