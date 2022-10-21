package server

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/peek"
	"github.com/wwqgtxx/wstunnel/tunnel"
	"github.com/wwqgtxx/wstunnel/utils"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  tunnel.BufSize,
	WriteBufferSize: tunnel.BufSize,
	WriteBufferPool: tunnel.WriteBufferPool,
}

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
				if err != nil {
					if errors.Is(err, os.ErrDeadlineExceeded) {
						_ = conn.SetReadDeadline(time.Time{})
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

func listenTcp(address string, sshFallbackAddress string, sshFallbackTimeout time.Duration) (net.Listener, error) {
	netLn, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	if len(sshFallbackAddress) > 0 {
		sshClientImpl := common.NewClientImpl(config.ClientConfig{TargetAddress: sshFallbackAddress})
		ln := &tcpListener{
			Listener:              netLn,
			sshFallbackClientImpl: sshClientImpl,
			sshFallbackTimeout:    sshFallbackTimeout,
			ch:                    make(chan acceptResult),
		}
		go ln.loop()
		return ln, nil
	}
	return netLn, nil
}

type server struct {
	bindAddress        string
	serverHandler      ServerHandler
	sshFallbackAddress string
	sshFallbackTimeout time.Duration
}

func (s *server) Start() {
	log.Println("New Server Listening on:", s.bindAddress)
	go func() {
		server := &http.Server{Addr: s.bindAddress, Handler: s.serverHandler}
		ln, err := listenTcp(s.bindAddress, s.sshFallbackAddress, s.sshFallbackTimeout)
		if err != nil {
			log.Println(err)
			return
		}
		err = server.Serve(ln)
		if err != nil {
			log.Println(err)
			return
		}
	}()
}

func (s *server) Addr() string {
	return s.bindAddress
}

func (s *server) CloneWithNewAddress(bindAddress string) common.Server {
	ns := *s
	ns.bindAddress = bindAddress
	return &ns
}

type ServerHandler http.Handler

type normalServerHandler struct {
	DestAddress string
}

func (s *normalServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		closeTcpHandle(w, r)
		return
	}

	log.Println("Incoming --> ", r.RemoteAddr, r.Header, s.DestAddress)

	ch := make(chan net.Conn)
	defer func() {
		if i, ok := <-ch; ok {
			_ = i.Close()
		}
	}()

	edBuf, responseHeader := utils.DecodeXray0rtt(r.Header)

	go func() {
		defer close(ch)
		tcp, err := net.Dial("tcp", s.DestAddress)
		if err != nil {
			log.Println(err)
			return
		}

		if len(edBuf) > 0 {
			_, err = tcp.Write(edBuf)
			if err != nil {
				log.Println(err)
				return
			}
		}
		ch <- tcp
	}()

	ws, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()

	tcp, ok := <-ch
	if !ok {
		return
	}
	defer tcp.Close()

	tunnel.TunnelTcpWs(tcp, ws)
}

type internalServerHandler struct {
	DestAddress string
	Proxy       string
	Client      common.Client
}

func (s *internalServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		closeTcpHandle(w, r)
		return
	}

	log.Println("Incoming --> ", r.RemoteAddr, r.Header, " --> ( [Client]", s.DestAddress, s.Proxy, ") --> ", s.Client.Target())

	ch := make(chan io.Closer)
	defer func() {
		if i, ok := <-ch; ok {
			_ = i.Close()
		}
	}()

	edBuf, responseHeader := utils.DecodeXray0rtt(r.Header)

	go func() {
		defer close(ch)
		// send inHeader to client for Xray's 0rtt ws
		ws2, err := s.Client.Dial(edBuf, r.Header)
		if err != nil {
			log.Println(err)
			return
		}
		ch <- ws2
	}()

	ws, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()
	source := ws.UnderlyingConn()

	ws2, ok := <-ch
	if !ok {
		return
	}
	defer ws2.Close()
	target := s.Client.ToRawConn(ws2)

	tunnel.TunnelTcpTcp(target, source)
}

func closeTcpHandle(writer http.ResponseWriter, request *http.Request) {
	h, ok := writer.(http.Hijacker)
	if !ok {
		return
	}
	netConn, _, err := h.Hijack()
	if err != nil {
		return
	}
	_ = netConn.Close()
}

func BuildServer(serverConfig config.ServerConfig) {
	mux := http.NewServeMux()
	hadRoot := false
	for port, _client := range common.PortToClient {
		wsPath := _client.GetServerWSPath()
		if len(wsPath) > 0 {
			serverConfig.Target = append(serverConfig.Target, config.ServerTargetConfig{
				WSPath:        wsPath,
				TargetAddress: net.JoinHostPort("127.0.0.1", port),
			})
		}
	}
	for _, target := range serverConfig.Target {
		if len(target.WSPath) == 0 {
			target.WSPath = "/"
		}
		host, port, err := net.SplitHostPort(target.TargetAddress)
		if err != nil {
			log.Println(err)
		}
		var sh ServerHandler
		_client, ok := common.PortToClient[port]
		if ok && (host == "127.0.0.1" || host == "localhost") {
			log.Println("Short circuit replace (",
				target.WSPath, "<->", target.TargetAddress,
				") to (",
				target.WSPath, "<->", _client.Target(), _client.Proxy(),
				")")
			sh = &internalServerHandler{
				DestAddress: target.TargetAddress,
				Proxy:       _client.Proxy(),
				Client:      _client,
			}
		} else {
			sh = &normalServerHandler{
				DestAddress: target.TargetAddress,
			}

		}
		if target.WSPath == "/" {
			hadRoot = true
		}
		mux.Handle(target.WSPath, sh)
	}
	if !hadRoot {
		mux.HandleFunc("/", closeTcpHandle)
	}
	var s common.Server
	s = &server{
		bindAddress:        serverConfig.BindAddress,
		serverHandler:      mux,
		sshFallbackAddress: serverConfig.SshFallbackAddress,
		sshFallbackTimeout: time.Duration(serverConfig.SshFallbackTimeout) * time.Second,
	}
	_, port, err := net.SplitHostPort(serverConfig.BindAddress)
	if err != nil {
		log.Println(err)
	}
	common.PortToServer[port] = s
}

func StartServers() {
	for _, server := range common.PortToServer {
		server.Start()
	}
}
