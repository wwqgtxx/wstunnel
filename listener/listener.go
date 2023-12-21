package listener

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback"
	"github.com/wwqgtxx/wstunnel/peek"
)

type Config struct {
	config.ListenerConfig
	config.ProxyConfig
	IsWebSocketListener bool
}

type tcpListener struct {
	net.Listener
	closeOnce sync.Once
	closed    chan struct{}
	ch        chan acceptResult
	fallback  *fallback.Fallback
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func (l *tcpListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
		_ = l.Listener.Close()
	})
	return nil
}

func (l *tcpListener) Accept() (net.Conn, error) {
	select {
	case r := <-l.ch:
		return r.conn, r.err
	case <-l.closed:
		return nil, errors.New("listener closed")
	}
}

func (l *tcpListener) loop() {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			select {
			case <-l.closed:
				return
			case l.ch <- acceptResult{conn: conn, err: err}:
			}
			continue
		}
		go func() {
			conn := peek.NewPeekConn(conn)
			if l.fallback.Handle(conn, nil, nil) {
				return
			}
			select {
			case <-l.closed:
				return
			case l.ch <- acceptResult{conn: conn, err: err}:
			}
		}()
	}
}

func ListenTcp(listenerConfig Config) (net.Listener, error) {
	lc := net.ListenConfig{}
	lc.SetMultipathTCP(true)
	netLn, err := lc.Listen(context.Background(), "tcp", listenerConfig.BindAddress)
	if err != nil {
		return nil, err
	}
	f, err := fallback.NewFallback(fallback.Config{
		FallbackConfig:      listenerConfig.FallbackConfig,
		ProxyConfig:         listenerConfig.ProxyConfig,
		IsWebSocketListener: listenerConfig.IsWebSocketListener,
	})
	if f != nil {
		ln := &tcpListener{
			Listener: netLn,
			closed:   make(chan struct{}),
			ch:       make(chan acceptResult),
			fallback: f,
		}
		go ln.loop()
		return ln, nil
	}
	return netLn, nil
}
