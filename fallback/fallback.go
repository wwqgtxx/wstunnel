package fallback

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback/ss2022"
	"github.com/wwqgtxx/wstunnel/fallback/ssaead"
	"github.com/wwqgtxx/wstunnel/fallback/tls"
	"github.com/wwqgtxx/wstunnel/fallback/vmessaead"
	"github.com/wwqgtxx/wstunnel/peek"
)

const (
	SSHStartString = "SSH-2"
	WSStartString  = "GET /" // websocket handshake actually is an HTTP GET

	PeekLength = 5
)

var NewClientImpl func(clientConfig config.ClientConfig) (common.ClientImpl, error)

type Config struct {
	config.FallbackConfig
	config.ProxyConfig
	IsWebSocketListener bool
}

type Fallback struct {
	sshClientImpl       common.ClientImpl
	sshFallbackTimeout  time.Duration
	wsClientImpl        common.ClientImpl
	unknownClientImpl   common.ClientImpl
	tlsTester           *tls.Tester[common.ClientImpl]
	ssTester            *ssaead.Tester[common.ClientImpl]
	ss2022Tester        *ss2022.Tester[common.ClientImpl]
	vmessTester         *vmessaead.Tester[common.ClientImpl]
	isWebSocketListener bool
}

func (f *Fallback) Handle(conn peek.Conn, edBuf []byte, inHeader http.Header) bool {
	if f == nil {
		return false
	}
	var buf []byte
	if f.sshFallbackTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(f.sshFallbackTimeout))
	}
	buf, err := conn.Peek(PeekLength)
	// move SetReadDeadline to accept() and tunnel()
	//_ = conn.SetReadDeadline(time.Time{})

	tunnel := func(clientImpl common.ClientImpl, name string, isTimeout bool) bool {
		_ = conn.SetReadDeadline(time.Time{})
		log.Println("Incoming", name, "Fallback --> ", conn.RemoteAddr(), " --> ", clientImpl.Target(), clientImpl.Proxy(), "isTimeout=", isTimeout)
		defer func() {
			_ = conn.Close()
		}()
		conn2, err := clientImpl.Dial(edBuf, inHeader)
		if err != nil {
			log.Println(err)
			return false
		}
		defer conn2.Close()
		conn2.TunnelTcp(conn)
		return true
	}
	accept := func() bool {
		_ = conn.SetReadDeadline(time.Time{})
		return false
	}

	if err != nil {
		if f.sshClientImpl != nil && IsTimeout(err) { // some client wait SSH server send handshake first (eg: motty).
			return tunnel(f.sshClientImpl, "SSH", true)
		}
		log.Println(err)
		return accept()
	}
	bufString := string(buf)
	//log.Println(bufString)
	switch bufString {
	case SSHStartString: // peek size == 5
		if f.sshClientImpl != nil {
			return tunnel(f.sshClientImpl, "SSH", false)
		}
	case WSStartString: // peek size == 5
		if f.wsClientImpl != nil {
			return tunnel(f.wsClientImpl, "WebSocket", false)
		}
		if f.isWebSocketListener {
			return accept()
		}
	}
	var ok bool
	if f.tlsTester != nil { // peek size == 5 + x
		ok, err = f.tlsTester.Test(conn, func(name string, clientImpl common.ClientImpl) {
			tunnel(clientImpl, fmt.Sprintf("TLS[%s]", name), false)
		})
		if err != nil && !IsTimeout(err) {
			log.Println(err)
			return accept()
		}
		if ok {
			return true
		}
	}
	if f.vmessTester != nil { // peek size == 16
		ok, err = f.vmessTester.Test(conn, func(name string, clientImpl common.ClientImpl) {
			tunnel(clientImpl, fmt.Sprintf("VMESS[%s]", name), false)
		})
		if err != nil && !IsTimeout(err) {
			log.Println(err)
			return accept()
		}
		if ok {
			return true
		}
	}
	if f.ssTester != nil { // peek size == (16/24/32) + 2 + 16
		ok, err = f.ssTester.Test(conn, func(name string, clientImpl common.ClientImpl) {
			tunnel(clientImpl, fmt.Sprintf("SS[%s]", name), false)
		})
		if err != nil && !IsTimeout(err) {
			log.Println(err)
			return accept()
		}
		if ok {
			return true
		}
	}
	if f.ss2022Tester != nil { // peek size == (16/24/32) + n*16 + 11 + 16
		ok, err = f.ss2022Tester.Test(conn, func(name string, clientImpl common.ClientImpl) {
			tunnel(clientImpl, fmt.Sprintf("SS2022[%s]", name), false)
		})
		if err != nil && !IsTimeout(err) {
			log.Println(err)
			return accept()
		}
		if ok {
			return true
		}
	}
	if f.unknownClientImpl != nil {
		return tunnel(f.unknownClientImpl, "Unknown", false)
	}
	return accept()
}

func NewFallback(fallbackConfig Config) (*Fallback, error) {
	var err error
	var clientImpl common.ClientImpl
	var sshClientImpl common.ClientImpl
	var wsClientImpl common.ClientImpl
	var unknownClientImpl common.ClientImpl
	var tlsTester *tls.Tester[common.ClientImpl]
	var ssTester *ssaead.Tester[common.ClientImpl]
	var ss2022Tester *ss2022.Tester[common.ClientImpl]
	var vmessTester *vmessaead.Tester[common.ClientImpl]
	if len(fallbackConfig.SshFallbackAddress) > 0 {
		sshClientImpl, err = NewClientImpl(config.ClientConfig{TargetAddress: fallbackConfig.SshFallbackAddress, ProxyConfig: fallbackConfig.ProxyConfig})
		if err != nil {
			return nil, err
		}
	}
	if len(fallbackConfig.WSFallbackAddress) > 0 {
		wsClientImpl, err = NewClientImpl(config.ClientConfig{TargetAddress: fallbackConfig.WSFallbackAddress, ProxyConfig: fallbackConfig.ProxyConfig})
		if err != nil {
			return nil, err
		}
	}
	if len(fallbackConfig.UnknownFallbackAddress) > 0 {
		unknownClientImpl, err = NewClientImpl(config.ClientConfig{TargetAddress: fallbackConfig.UnknownFallbackAddress, ProxyConfig: fallbackConfig.ProxyConfig})
		if err != nil {
			return nil, err
		}
	}
	if len(fallbackConfig.TLSFallbackAddress) > 0 {
		fallbackConfig.TLSFallback = append(fallbackConfig.TLSFallback, config.TLSFallbackConfig{
			SNI:     "",
			Address: fallbackConfig.TLSFallbackAddress,
		})
	}
	if len(fallbackConfig.TLSFallback) > 0 {
		tlsTester = tls.NewTester[common.ClientImpl]()
		for _, tlsFallbackConfig := range fallbackConfig.TLSFallback {
			sni := tlsFallbackConfig.SNI
			clientImpl, err = NewClientImpl(config.ClientConfig{
				TargetAddress: tlsFallbackConfig.Address,
				Mtp:           tlsFallbackConfig.Mtp,
				ProxyConfig:   fallbackConfig.ProxyConfig,
			})
			if err != nil {
				return nil, err
			}
			if c, ok := clientImpl.(interface{ SNI() string }); ok {
				sni = c.SNI()
			}
			if len(sni) == 0 && len(tlsFallbackConfig.Mtp) > 0 {
				return nil, fmt.Errorf("not faketls mtp: %s", tlsFallbackConfig.Mtp)
			}
			err = tlsTester.Add(sni, clientImpl)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(fallbackConfig.SSFallback) > 0 {
		ssTester = ssaead.NewTester[common.ClientImpl]()
		for _, ssFallbackConfig := range fallbackConfig.SSFallback {
			clientImpl, err = NewClientImpl(config.ClientConfig{TargetAddress: ssFallbackConfig.Address, ProxyConfig: fallbackConfig.ProxyConfig})
			if err != nil {
				return nil, err
			}
			err = ssTester.Add(
				ssFallbackConfig.Name,
				ssFallbackConfig.Method,
				ssFallbackConfig.Password,
				clientImpl,
			)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(fallbackConfig.SS2022Fallback) > 0 {
		ss2022Tester = ss2022.NewTester[common.ClientImpl]()
		for _, ss2022FallbackConfig := range fallbackConfig.SS2022Fallback {
			clientImpl, err = NewClientImpl(config.ClientConfig{TargetAddress: ss2022FallbackConfig.Address, ProxyConfig: fallbackConfig.ProxyConfig})
			if err != nil {
				return nil, err
			}
			err = ss2022Tester.Add(
				ss2022FallbackConfig.Name,
				ss2022FallbackConfig.Method,
				ss2022FallbackConfig.Password,
				clientImpl,
			)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(fallbackConfig.VmessFallback) > 0 {
		vmessTester = vmessaead.NewTester[common.ClientImpl]()
		for _, vmessFallbackConfig := range fallbackConfig.VmessFallback {
			clientImpl, err = NewClientImpl(config.ClientConfig{TargetAddress: vmessFallbackConfig.Address, ProxyConfig: fallbackConfig.ProxyConfig})
			if err != nil {
				return nil, err
			}
			err = vmessTester.Add(
				vmessFallbackConfig.Name,
				vmessFallbackConfig.UUID,
				clientImpl,
			)
			if err != nil {
				return nil, err
			}
		}
	}
	if sshClientImpl != nil || wsClientImpl != nil || unknownClientImpl != nil || tlsTester != nil || ssTester != nil || vmessTester != nil {
		f := &Fallback{
			sshClientImpl:       sshClientImpl,
			sshFallbackTimeout:  time.Duration(fallbackConfig.SshFallbackTimeout) * time.Second,
			wsClientImpl:        wsClientImpl,
			unknownClientImpl:   unknownClientImpl,
			tlsTester:           tlsTester,
			ssTester:            ssTester,
			ss2022Tester:        ss2022Tester,
			vmessTester:         vmessTester,
			isWebSocketListener: fallbackConfig.IsWebSocketListener,
		}
		return f, nil
	}
	return nil, nil
}

type TimeoutError interface {
	Timeout() bool
}

func IsTimeout(err error) bool {
	// gorilla/websocket has a hideTempErr() os we can't use errors.Is(err, os.ErrDeadlineExceeded)
	var t TimeoutError
	if errors.As(err, &t) && t.Timeout() {
		return true
	}
	return false
}
