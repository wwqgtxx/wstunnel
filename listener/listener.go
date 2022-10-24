package listener

import (
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

const (
	SSHStartString = "SSH"
	TLSStartString = "\x16\x03\x01" // TLS1.0 Handshake (works on TLS1.0, 1.1, 1.2, 1.3)
	WSStartString  = "GET"          // websocket handshake actually is an HTTP GET

	PeekLength = 3
)

var NewClientImpl func(clientConfig config.ClientConfig) common.ClientImpl

type Config struct {
	config.ListenerConfig
	config.ProxyConfig
	IsWebSocketListener bool
}

type tcpListener struct {
	net.Listener
	closed *atomic.Bool
	ch     chan acceptResult

	sshClientImpl       common.ClientImpl
	sshFallbackTimeout  time.Duration
	tlsClientImpl       common.ClientImpl
	wsClientImpl        common.ClientImpl
	unknownClientImpl   common.ClientImpl
	isWebSocketListener bool
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
			var buf []byte
			if l.sshFallbackTimeout > 0 {
				_ = conn.SetReadDeadline(time.Now().Add(l.sshFallbackTimeout))
			}
			conn, buf, err = peek.Peek(conn, PeekLength)
			_ = conn.SetReadDeadline(time.Time{})

			tunnel := func(clientImpl common.ClientImpl, name string, isTimeout bool) {
				log.Println("Incoming", name, "Fallback --> ", conn.RemoteAddr(), " --> ", clientImpl.Target(), clientImpl.Proxy(), "isTimeout=", isTimeout)
				defer func() {
					_ = conn.Close()
				}()
				conn2, err := clientImpl.Dial(nil, nil)
				if err != nil {
					log.Println(err)
					return
				}
				conn2.TunnelTcp(conn)
			}
			accept := func() {
				l.ch <- acceptResult{conn: conn, err: err}
			}

			if err != nil {
				if l.sshClientImpl != nil && errors.Is(err, os.ErrDeadlineExceeded) { // some client wait SSH server send handshake first (eg: motty).
					tunnel(l.sshClientImpl, "SSH", true)
					return
				}
				log.Println(err)
				return
			}
			ws := false
			bufString := string(buf)
			//log.Println(bufString)
			switch bufString {
			case SSHStartString:
				if l.sshClientImpl != nil {
					tunnel(l.sshClientImpl, "SSH", false)
					return
				}
			case TLSStartString:
				if l.tlsClientImpl != nil {
					tunnel(l.tlsClientImpl, "TLS", false)
					return
				}
			case WSStartString:
				if l.wsClientImpl != nil {
					tunnel(l.wsClientImpl, "WebSocket", false)
					return
				}
				ws = true
			}
			if l.unknownClientImpl != nil {
				if l.isWebSocketListener && ws {
					accept()
					return
				} else {
					tunnel(l.unknownClientImpl, "Unknown", false)
					return
				}
			}
			accept()
		}()
	}
}

func ListenTcp(listenerConfig Config) (net.Listener, error) {
	netLn, err := net.Listen("tcp", listenerConfig.BindAddress)
	if err != nil {
		return nil, err
	}
	var sshClientImpl common.ClientImpl
	var tlsClientImpl common.ClientImpl
	var wsClientImpl common.ClientImpl
	var unknownClientImpl common.ClientImpl
	if len(listenerConfig.SshFallbackAddress) > 0 {
		sshClientImpl = NewClientImpl(config.ClientConfig{TargetAddress: listenerConfig.SshFallbackAddress, ProxyConfig: listenerConfig.ProxyConfig})
	}
	if len(listenerConfig.TLSFallbackAddress) > 0 {
		tlsClientImpl = NewClientImpl(config.ClientConfig{TargetAddress: listenerConfig.TLSFallbackAddress, ProxyConfig: listenerConfig.ProxyConfig})
	}
	if len(listenerConfig.WSFallbackAddress) > 0 {
		wsClientImpl = NewClientImpl(config.ClientConfig{TargetAddress: listenerConfig.WSFallbackAddress, ProxyConfig: listenerConfig.ProxyConfig})
	}
	if len(listenerConfig.UnknownFallbackAddress) > 0 {
		unknownClientImpl = NewClientImpl(config.ClientConfig{TargetAddress: listenerConfig.UnknownFallbackAddress, ProxyConfig: listenerConfig.ProxyConfig})
	}
	if tlsClientImpl != nil || sshClientImpl != nil || wsClientImpl != nil || unknownClientImpl != nil {
		ln := &tcpListener{
			Listener:            netLn,
			sshClientImpl:       sshClientImpl,
			sshFallbackTimeout:  time.Duration(listenerConfig.SshFallbackTimeout) * time.Second,
			tlsClientImpl:       tlsClientImpl,
			wsClientImpl:        wsClientImpl,
			unknownClientImpl:   unknownClientImpl,
			isWebSocketListener: listenerConfig.IsWebSocketListener,
			ch:                  make(chan acceptResult),
		}
		go ln.loop()
		return ln, nil
	}
	return netLn, nil
}
