package udp

import (
	"net"
	"net/netip"
)

type EnhancePacketConn interface {
	net.PacketConn
	WaitReadFrom() (data []byte, put func(), addr netip.AddrPort, err error)
}

func NewEnhancePacketConn(pc net.PacketConn) EnhancePacketConn {
	if udpConn, isUDPConn := pc.(*net.UDPConn); isUDPConn {
		return newEnhancePacketConn(udpConn)
	}
	if enhancePC, isEnhancePC := pc.(EnhancePacketConn); isEnhancePC {
		return enhancePC
	}
	return &enhancePacketConn{PacketConn: pc}
}

type enhancePacketConn struct {
	net.PacketConn
}

func (c *enhancePacketConn) WaitReadFrom() (data []byte, put func(), addr netip.AddrPort, err error) {
	return waitReadFrom(c.PacketConn)
}

func waitReadFrom(pc net.PacketConn) (data []byte, put func(), addr netip.AddrPort, err error) {
	readBuf := BufPool.Get().([]byte)
	put = func() {
		BufPool.Put(readBuf)
	}
	var readN int
	var udpAddr net.Addr
	readN, udpAddr, err = pc.ReadFrom(readBuf)
	if udpAddr, ok := udpAddr.(interface{ AddrPort() netip.AddrPort }); ok {
		addr = udpAddr.AddrPort()
	}
	if readN > 0 {
		data = readBuf[:readN]
	} else {
		put()
		put = nil
	}
	return
}
