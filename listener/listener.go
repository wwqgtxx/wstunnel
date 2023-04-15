package listener

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/peek"
	"github.com/wwqgtxx/wstunnel/tester/ssaead"
	"github.com/wwqgtxx/wstunnel/tester/vmessaead"
	"github.com/wwqgtxx/wstunnel/utils"

	"github.com/sagernet/tfo-go"
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
	ssTester            *ssaead.Tester
	vmessTester         *vmessaead.Tester
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
			conn := peek.NewPeekConn(conn)
			if l.sshFallbackTimeout > 0 {
				_ = conn.SetReadDeadline(time.Now().Add(l.sshFallbackTimeout))
			}
			buf, err = conn.Peek(PeekLength)
			// move SetReadDeadline to accept() and tunnel()
			//_ = conn.SetReadDeadline(time.Time{})

			tunnel := func(clientImpl common.ClientImpl, name string, isTimeout bool) {
				_ = conn.SetReadDeadline(time.Time{})
				log.Println("Incoming", name, "Fallback --> ", conn.RemoteAddr(), " --> ", clientImpl.Target(), clientImpl.Proxy(), "isTimeout=", isTimeout)
				defer func() {
					_ = conn.Close()
				}()
				conn2, err := clientImpl.Dial(nil, nil)
				if err != nil {
					log.Println(err)
					return
				}
				defer conn2.Close()
				conn2.TunnelTcp(conn)
			}
			accept := func() {
				_ = conn.SetReadDeadline(time.Time{})
				l.ch <- acceptResult{conn: conn, err: err}
			}
			onError := func(err error) {
				if l.sshClientImpl != nil && errors.Is(err, os.ErrDeadlineExceeded) { // some client wait SSH server send handshake first (eg: motty).
					tunnel(l.sshClientImpl, "SSH", true)
					return
				}
				log.Println(err)
				return
			}

			if err != nil {
				onError(err)
			}
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
					sni := utils.ClientHelloServerName(conn)
					tunnel(l.tlsClientImpl, fmt.Sprintf("TLS[%s]", sni), false)
					return
				}
			case WSStartString:
				if l.wsClientImpl != nil {
					tunnel(l.wsClientImpl, "WebSocket", false)
					return
				}
				if l.isWebSocketListener {
					accept()
					return
				}
			}
			if l.vmessTester != nil { // peek size == 16
				ok, err := l.vmessTester.Test(conn, func(name string, clientImpl common.ClientImpl) {
					tunnel(clientImpl, fmt.Sprintf("VMESS[%s]", name), false)
				})
				if err != nil {
					onError(err)
					return
				}
				if ok {
					return
				}
			}
			if l.ssTester != nil { // peek size == (16/24/32) + 2 + 16
				ok, err := l.ssTester.Test(conn, func(name string, clientImpl common.ClientImpl) {
					tunnel(clientImpl, fmt.Sprintf("SS[%s]", name), false)
				})
				if err != nil {
					onError(err)
					return
				}
				if ok {
					return
				}
			}
			if l.unknownClientImpl != nil {
				tunnel(l.unknownClientImpl, "Unknown", false)
				return
			}
			accept()
		}()
	}
}

func ListenTcp(listenerConfig Config) (net.Listener, error) {
	netLn, err := tfo.Listen("tcp", listenerConfig.BindAddress)
	if err != nil {
		return nil, err
	}
	var sshClientImpl common.ClientImpl
	var tlsClientImpl common.ClientImpl
	var wsClientImpl common.ClientImpl
	var unknownClientImpl common.ClientImpl
	var ssTester *ssaead.Tester
	var vmessTester *vmessaead.Tester
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
	if len(listenerConfig.SSFallback) > 0 {
		ssTester = ssaead.NewTester()
		for _, ssFallbackConfig := range listenerConfig.SSFallback {
			err = ssTester.Add(
				ssFallbackConfig.Name,
				ssFallbackConfig.Method,
				ssFallbackConfig.Password,
				NewClientImpl(config.ClientConfig{TargetAddress: ssFallbackConfig.Address, ProxyConfig: listenerConfig.ProxyConfig}),
			)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(listenerConfig.VmessFallback) > 0 {
		vmessTester = vmessaead.NewTester()
		for _, vmessFallbackConfig := range listenerConfig.VmessFallback {
			err = vmessTester.Add(
				vmessFallbackConfig.Name,
				vmessFallbackConfig.UUID,
				NewClientImpl(config.ClientConfig{TargetAddress: vmessFallbackConfig.Address, ProxyConfig: listenerConfig.ProxyConfig}),
			)
			if err != nil {
				return nil, err
			}
		}
	}
	if tlsClientImpl != nil || sshClientImpl != nil || wsClientImpl != nil || unknownClientImpl != nil {
		ln := &tcpListener{
			Listener:            netLn,
			sshClientImpl:       sshClientImpl,
			sshFallbackTimeout:  time.Duration(listenerConfig.SshFallbackTimeout) * time.Second,
			tlsClientImpl:       tlsClientImpl,
			wsClientImpl:        wsClientImpl,
			unknownClientImpl:   unknownClientImpl,
			ssTester:            ssTester,
			vmessTester:         vmessTester,
			isWebSocketListener: listenerConfig.IsWebSocketListener,
			ch:                  make(chan acceptResult),
		}
		go ln.loop()
		return ln, nil
	}
	return netLn, nil
}
