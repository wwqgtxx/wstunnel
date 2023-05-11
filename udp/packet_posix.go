//go:build !windows

package udp

import (
	"io"
	"net"
	"net/netip"
	"strconv"
	"syscall"
)

type enhanceUDPConn struct {
	*net.UDPConn
	rawConn syscall.RawConn
}

func (c *enhanceUDPConn) WaitReadFrom() (data []byte, put func(), addr netip.AddrPort, err error) {
	if c.rawConn == nil {
		c.rawConn, _ = c.UDPConn.SyscallConn()
	}
	var ip netip.Addr
	var port int
	var readErr error
	err = c.rawConn.Read(func(fd uintptr) (done bool) {
		readBuf := BufPool.Get().([]byte)
		put = func() {
			BufPool.Put(readBuf)
		}
		var readFrom syscall.Sockaddr
		var readN int
		readN, _, _, readFrom, readErr = syscall.Recvmsg(int(fd), readBuf, nil, 0)
		if readN > 0 {
			data = readBuf[:readN]
		} else {
			put()
			put = nil
		}
		if readErr == syscall.EAGAIN {
			return false
		}
		if readFrom != nil {
			switch from := readFrom.(type) {
			case *syscall.SockaddrInet4:
				ip = netip.AddrFrom4(from.Addr)
				port = from.Port
			case *syscall.SockaddrInet6:
				ip = netip.AddrFrom16(from.Addr).WithZone(strconv.FormatInt(int64(from.ZoneId), 10))
				port = from.Port
			}
		}
		if readN == 0 {
			readErr = io.EOF
		}
		return true
	})
	if err != nil {
		return
	}
	if readErr != nil {
		err = readErr
		return
	}
	addr = netip.AddrPortFrom(ip, uint16(port))
	return
}
