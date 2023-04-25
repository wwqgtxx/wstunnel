package tunnel

import (
	"io"
	"log"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/wwqgtxx/wstunnel/peek"
	"github.com/wwqgtxx/wstunnel/utils"

	"github.com/gorilla/websocket"
)

const (
	BufSize = 4096
)

var (
	BufPool         = sync.Pool{New: func() any { return make([]byte, BufSize) }}
	WriteBufferPool = &sync.Pool{}
)

func TcpWs(tcp net.Conn, ws *websocket.Conn) {
	setKeepAlive(tcp)
	setKeepAlive(ws.UnderlyingConn())

	exit := make(chan struct{}, 1)

	go func() {
		_, err := Copy(utils.NewWsWriter(ws), tcp)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		_ = ws.SetReadDeadline(time.Now())
		exit <- struct{}{}
	}()

	_, err := Copy(tcp, utils.NewWsReader(ws))
	if err != nil && err == io.EOF {
		log.Println(err)
	}
	_ = tcp.SetReadDeadline(time.Now())

	<-exit
}

func TcpTcp(tcp1 net.Conn, tcp2 net.Conn) {
	setKeepAlive(tcp1)
	setKeepAlive(tcp2)

	exit := make(chan struct{}, 1)

	go func() {
		_, err := Copy(tcp1, tcp2)
		if err != nil && err == io.EOF {
			log.Println(err)
		}
		_ = tcp1.SetReadDeadline(time.Now())
		exit <- struct{}{}
	}()

	_, err := Copy(tcp2, tcp1)
	if err != nil && err == io.EOF {
		log.Println(err)
	}
	_ = tcp2.SetReadDeadline(time.Now())

	<-exit
}

func Copy(dst io.Writer, src io.Reader) (written int64, err error) {
	dst = peek.ToWriter(dst)
	for {
		src = peek.ToReader(src)
		if rc, ok := src.(peek.ReadCached); ok {
			b := rc.ReadCached()
			if len(b) > 0 {
				var n int
				n, err = dst.Write(b)
				written += int64(n)
				if err != nil {
					return
				}
				continue
			}
		}
		break
	}
	if srcSyscall, ok := src.(syscall.Conn); ok {
		if srcRaw, sErr := srcSyscall.SyscallConn(); sErr == nil {
			var handle bool
			var n int64
			if dstSyscall, ok := dst.(syscall.Conn); ok {
				if dstRaw, sErr := dstSyscall.SyscallConn(); sErr == nil {
					handle, n, err = splice(src, srcRaw, dst, dstRaw)
					written += n
					if handle {
						return
					}
				}
			}
			handle, n, err = syscallCopy(src, srcRaw, dst)
			written += n
			if handle {
				return
			}
		}
	}
	var n int64
	n, err = stdCopy(dst, src)
	written += n
	return
}

func setKeepAlive(c net.Conn) {
	writer := peek.ToWriter(c) // writer is always the underlying conn in peek.Conn
	if conn, ok := writer.(interface{ SetKeepAlive(keepalive bool) error }); ok {
		_ = conn.SetKeepAlive(true)
	}
	if conn, ok := writer.(interface{ SetKeepAlivePeriod(d time.Duration) error }); ok {
		_ = conn.SetKeepAlivePeriod(30 * time.Second)
	}
}
