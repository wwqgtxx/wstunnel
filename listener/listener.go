package listener

import (
	"bytes"
	"errors"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/peek"
)

type tcpListener struct {
	net.Listener
	closed *atomic.Bool
	ch     chan acceptResult

	sshFallbackClientImpl common.ClientImpl
	sshFallbackTimeout    time.Duration
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func (l *tcpListener) Close() error {
	if !l.closed.Swap(true) {
		return l.Listener.Close()
	}
	return nil
}

func (l *tcpListener) Accept() (net.Conn, error) {
	if r, ok := <-l.ch; ok {
		return r.conn, r.err
	}
	return nil, errors.New("listener closed")
}

func (l *tcpListener) loop() {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			if l.closed.Load() {
				close(l.ch)
				return
			}
			l.ch <- acceptResult{conn: conn, err: err}
			continue
		}
		go func() {
			if l.sshFallbackClientImpl != nil {
				tunnelSSH := func(isTimeout bool) {
					log.Println("Incoming SSH Fallback --> ", conn.RemoteAddr(), " --> ", l.sshFallbackClientImpl.Target(), l.sshFallbackClientImpl.Proxy(), "isTimeout=", isTimeout)
					defer func() {
						_ = conn.Close()
					}()
					conn2, err := l.sshFallbackClientImpl.Dial(nil, nil)
					if err != nil {
						log.Println(err)
						return
					}
					l.sshFallbackClientImpl.Tunnel(conn, conn2)
				}

				buf := make([]byte, 3)
				_ = conn.SetReadDeadline(time.Now().Add(l.sshFallbackTimeout))
				conn, err = peek.Peek(conn, buf)
				_ = conn.SetReadDeadline(time.Time{})
				if err != nil {
					if errors.Is(err, os.ErrDeadlineExceeded) {
						tunnelSSH(true)
						return
					}
					log.Println(err)
					return
				}
				//log.Println(string(buf))
				if bytes.Equal(buf, []byte("SSH")) {
					tunnelSSH(false)
					return
				}
			}
			l.ch <- acceptResult{conn: conn, err: err}
		}()

	}
}

func ListenTcp(address string, sshFallbackConfig config.SshFallbackConfig) (net.Listener, error) {
	netLn, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	if len(sshFallbackConfig.SshFallbackAddress) > 0 {
		sshClientImpl := common.NewClientImpl(config.ClientConfig{TargetAddress: sshFallbackConfig.SshFallbackAddress})
		ln := &tcpListener{
			Listener:              netLn,
			sshFallbackClientImpl: sshClientImpl,
			sshFallbackTimeout:    time.Duration(sshFallbackConfig.SshFallbackTimeout) * time.Second,
			ch:                    make(chan acceptResult),
		}
		go ln.loop()
		return ln, nil
	}
	return netLn, nil
}
