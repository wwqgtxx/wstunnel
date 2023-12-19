//go:build !windows

package udp

import (
	"net"
	"net/netip"
	"strconv"
	"syscall"
)

type enhanceUDPConn struct {
	*net.UDPConn
	rawConn   syscall.RawConn
	data      []byte
	put       func()
	addr      netip.AddrPort
	readErr   error
	boundRead func(fd uintptr) (done bool)
}

func newEnhancePacketConn(udpConn *net.UDPConn) EnhancePacketConn {
	c := &enhanceUDPConn{UDPConn: udpConn}
	c.rawConn, _ = udpConn.SyscallConn()
	c.boundRead = c.read
	return c
}

func (c *enhanceUDPConn) read(fd uintptr) (done bool) {
	readBuf := BufPool.Get().([]byte)
	c.put = func() {
		BufPool.Put(readBuf)
	}
	var readFrom syscall.Sockaddr
	var readN int
	readN, _, _, readFrom, c.readErr = syscall.Recvmsg(int(fd), readBuf, nil, 0)
	if readN > 0 {
		c.data = readBuf[:readN]
	} else {
		c.put()
		c.put = nil
		c.data = nil
	}
	if c.readErr == syscall.EAGAIN {
		return false
	}
	if readFrom != nil {
		switch from := readFrom.(type) {
		case *syscall.SockaddrInet4:
			ip := netip.AddrFrom4(from.Addr)
			port := from.Port
			c.addr = netip.AddrPortFrom(ip, uint16(port))
		case *syscall.SockaddrInet6:
			ip := netip.AddrFrom16(from.Addr).WithZone(strconv.FormatInt(int64(from.ZoneId), 10))
			port := from.Port
			c.addr = netip.AddrPortFrom(ip, uint16(port))
		}
	}
	// udp should not convert readN == 0 to io.EOF
	//if readN == 0 {
	//	c.readErr = io.EOF
	//}
	return true
}

func (c *enhanceUDPConn) WaitReadFrom() (data []byte, put func(), addr netip.AddrPort, err error) {
	err = c.rawConn.Read(c.boundRead)
	if err != nil {
		return
	}
	if c.readErr != nil {
		err = c.readErr
		return
	}
	data = c.data
	put = c.put
	addr = c.addr
	return
}
