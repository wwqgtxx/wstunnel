//go:build windows

package udp

import (
	"net"
	"net/netip"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

type enhanceUDPConn struct {
	*net.UDPConn
	rawConn syscall.RawConn
}

func newEnhancePacketConn(udpConn *net.UDPConn) EnhancePacketConn {
	c := &enhanceUDPConn{UDPConn: udpConn}
	c.rawConn, _ = udpConn.SyscallConn()
	return c
}

func (c *enhanceUDPConn) WaitReadFrom() (data []byte, put func(), addr netip.AddrPort, err error) {
	if c.rawConn == nil {
		c.rawConn, _ = c.UDPConn.SyscallConn()
	}
	var readErr error
	hasData := false
	err = c.rawConn.Read(func(fd uintptr) (done bool) {
		if !hasData {
			hasData = true
			// golang's internal/poll.FD.RawRead will Use a zero-byte read as a way to get notified when this
			// socket is readable if we return false. So the `recvfrom` syscall will not block the system thread.
			return false
		}
		readBuf := BufPool.Get().([]byte)
		put = func() {
			BufPool.Put(readBuf)
		}
		var readFrom windows.Sockaddr
		var readN int
		readN, readFrom, readErr = windows.Recvfrom(windows.Handle(fd), readBuf, 0)
		if readN > 0 {
			data = readBuf[:readN]
		} else {
			put()
			put = nil
			data = nil
		}
		if readErr == windows.WSAEWOULDBLOCK {
			return false
		}
		if readFrom != nil {
			switch from := readFrom.(type) {
			case *windows.SockaddrInet4:
				ip := from.Addr // copy from.Addr; ip escapes, so this line allocates 4 bytes
				addr = netip.AddrPortFrom(netip.AddrFrom4(ip), uint16(from.Port))
			case *windows.SockaddrInet6:
				ip := from.Addr // copy from.Addr; ip escapes, so this line allocates 16 bytes
				addr = netip.AddrPortFrom(netip.AddrFrom16(ip).WithZone(strconv.FormatInt(int64(from.ZoneId), 10)), uint16(from.Port))
			}
		}
		// udp should not convert readN == 0 to io.EOF
		//if readN == 0 {
		//	readErr = io.EOF
		//}
		hasData = false
		return true
	})
	if err != nil {
		return
	}
	if readErr != nil {
		err = readErr
		return
	}
	return
}
