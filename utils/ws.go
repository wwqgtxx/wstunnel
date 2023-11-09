package utils

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wwqgtxx/wstunnel/peek"
	"github.com/wwqgtxx/wstunnel/proxy"

	"github.com/gobwas/pool/pbufio"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

type WebsocketConn struct {
	net.Conn
	isRaw          bool
	state          ws.State
	reader         *wsutil.Reader
	controlHandler wsutil.FrameHandlerFunc
}

func NewWebsocketConn(conn net.Conn, state ws.State, isRaw bool) *WebsocketConn {
	controlHandler := wsutil.ControlFrameHandler(conn, state)
	return &WebsocketConn{
		Conn:  conn,
		isRaw: isRaw,
		state: state,
		reader: &wsutil.Reader{
			Source:          conn,
			State:           state,
			SkipHeaderCheck: true,
			CheckUTF8:       false,
			OnIntermediate:  controlHandler,
		},
		controlHandler: controlHandler,
	}
}

func (w *WebsocketConn) Read(p []byte) (n int, err error) {
	if w.isRaw {
		return w.Conn.Read(p)
	}
	var header ws.Header
	for {
		n, err = w.reader.Read(p)
		// in gobwas/ws: "The error is io.EOF only if all of message bytes were read."
		// but maybe next frame still have data, so drop it
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if !errors.Is(err, wsutil.ErrNoFrameAdvance) {
			return
		}
		header, err = w.reader.NextFrame()
		if err != nil {
			return
		}
		if header.OpCode.IsControl() {
			err = w.controlHandler(header, w.reader)
			if err != nil {
				return
			}
			continue
		}
		if header.OpCode&(ws.OpBinary|ws.OpText) == 0 {
			log.Println("unknown msgType")
			err = w.reader.Discard()
			if err != nil {
				return
			}
			continue
		}
	}
}

func (w *WebsocketConn) writeMessage(op ws.OpCode, p []byte) error {
	writer := pbufio.GetWriter(w.Conn, wsutil.DefaultWriteBuffer) // using a bufio.Writer to combine Header and Payload
	defer pbufio.PutWriter(writer)
	err := wsutil.WriteMessage(writer, w.state, op, p)
	if err != nil {
		return err
	}
	return writer.Flush()
}

func (w *WebsocketConn) Write(p []byte) (n int, err error) {
	if w.isRaw {
		return w.Conn.Write(p)
	}
	err = w.writeMessage(ws.OpBinary, p)
	if err != nil {
		return
	}
	n = len(p)
	return
}

func (w *WebsocketConn) Close() error {
	if w.isRaw {
		return w.Conn.Close()
	}
	var e []string
	_ = w.Conn.SetWriteDeadline(time.Now().Add(time.Second * 5))
	if err := w.writeMessage(ws.OpClose, ws.NewCloseFrameBody(ws.StatusNormalClosure, "")); err != nil {
		e = append(e, err.Error())
	}
	if err := w.Conn.Close(); err != nil {
		e = append(e, err.Error())
	}
	if len(e) > 0 {
		return fmt.Errorf("failed to close connection: %s", strings.Join(e, ","))
	}
	return nil
}

func (w *WebsocketConn) ReaderReplaceable() bool {
	return w.isRaw
}

func (w *WebsocketConn) ToReader() io.Reader {
	return w.Conn
}

func (w *WebsocketConn) WriterReplaceable() bool {
	return w.isRaw
}

func (w *WebsocketConn) ToWriter() io.Writer {
	return w.Conn
}

func ServerWebsocketUpgrade(w http.ResponseWriter, r *http.Request) (*WebsocketConn, error) {
	var conn net.Conn
	var rw *bufio.ReadWriter
	var err error
	isRaw := IsV2rayHttpUpdate(r)
	if isRaw { // v2ray-http-upgrade
		w.Header().Set("Connection", "upgrade")
		w.Header().Set("Upgrade", "websocket")
		w.WriteHeader(http.StatusSwitchingProtocols)
		if flusher, isFlusher := w.(interface{ FlushError() error }); isFlusher {
			err = flusher.FlushError()
			if err != nil {
				return nil, fmt.Errorf("flush response: %w", err)
			}
		}
		hijacker, canHijack := w.(http.Hijacker)
		if !canHijack {
			return nil, errors.New("invalid connection, maybe HTTP/2")
		}
		conn, rw, err = hijacker.Hijack()
		if err != nil {
			return nil, fmt.Errorf("hijack failed: %w", err)
		}
	} else {
		conn, rw, _, err = ws.UpgradeHTTP(r, w)
		if err != nil {
			return nil, fmt.Errorf("ws upgrade failed: %w", err)
		}
	}

	// gobwas/ws will flush rw.Writer, so we only need warp rw.Reader
	conn = peek.WarpConnWithBioReader(conn, rw.Reader)

	return NewWebsocketConn(conn, ws.StateServerSide, isRaw), nil
}

func IsWebSocketUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") == "websocket"
}

func IsV2rayHttpUpdate(r *http.Request) bool {
	return IsWebSocketUpgrade(r) && r.Header.Get("Sec-WebSocket-Key") == ""
}

func ClientWebsocketDial(uri url.URL, cHeaders http.Header, netDial proxy.NetDialerFunc, tlsConfig *tls.Config, dialTimeout time.Duration) (*WebsocketConn, http.Header, error) {
	hostname := uri.Hostname()
	wsDialer := ws.Dialer{
		Timeout: dialTimeout,
		NetDial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if _, port, err := net.SplitHostPort(addr); err == nil {
				addr = net.JoinHostPort(hostname, port)
			}
			return netDial(network, addr)
		},
		TLSConfig: tlsConfig,
	}

	headers := http.Header{}
	headers.Set("User-Agent", "Go-http-client/1.1") // match golang's net/http
	if cHeaders != nil {
		for k := range cHeaders {
			headers.Add(k, cHeaders.Get(k))
		}
	}

	// gobwas/ws will check server's response "Sec-Websocket-Protocol" so must add Protocols to ws.Dialer
	// if not will cause ws.ErrHandshakeBadSubProtocol
	if secProtocol := headers.Get("Sec-WebSocket-Protocol"); len(secProtocol) > 0 {
		// gobwas/ws will set "Sec-Websocket-Protocol" according dialer.Protocols
		// to avoid send repeatedly don't set it to headers
		headers.Del("Sec-WebSocket-Protocol")
		wsDialer.Protocols = []string{secProtocol}
	}

	// gobwas/ws send "Host" directly in Upgrade() by `httpWriteHeader(bw, headerHost, u.Host)`
	// if headers has "Host" will send repeatedly
	if host := headers.Get("Host"); host != "" {
		headers.Del("Host")
		uri.Host = host
	}
	wsDialer.Header = ws.HandshakeHeaderHTTP(headers)

	conn, br, _, err := wsDialer.Dial(context.Background(), uri.String())
	if err != nil {
		return nil, headers, err
	}

	// some bytes which could be written by the peer right after response and be caught by us during buffered read,
	// so we need warp Conn with bio.Reader
	conn = peek.WarpConnWithBioReader(conn, br)

	return NewWebsocketConn(conn, ws.StateClientSide, false), headers, nil
}
