package utils

import (
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
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
	"github.com/zhangyunhao116/fastrand"
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
	w.Header().Set("Connection", "upgrade")
	w.Header().Set("Upgrade", "websocket")
	if !isRaw {
		w.Header().Set("Sec-Websocket-Accept", getSecAccept(r.Header.Get("Sec-WebSocket-Key")))
	}
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

	// rw.Writer was flushed, so we only need warp rw.Reader
	conn = peek.WarpConnWithBioReader(conn, rw.Reader)

	return NewWebsocketConn(conn, ws.StateServerSide, isRaw), nil
}

func IsWebSocketUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") == "websocket"
}

func IsV2rayHttpUpdate(r *http.Request) bool {
	return IsWebSocketUpgrade(r) && r.Header.Get("Sec-WebSocket-Key") == ""
}

func ClientWebsocketDial(ctx context.Context, uri url.URL, cHeaders http.Header, dialer proxy.ContextDialer, tlsConfig *tls.Config, v2rayHttpUpgrade bool) (net.Conn, http.Header, error) {
	hostname := uri.Hostname()
	port := uri.Port()
	if port == "" {
		switch uri.Scheme {
		case "ws":
			port = "80"
		case "wss":
			port = "443"
		}
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(hostname, port))
	if err != nil {
		return nil, nil, err
	}

	if uri.Scheme == "wss" {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.NextProtos = []string{"http/1.1"}
		if tlsConfig.ServerName == "" && !tlsConfig.InsecureSkipVerify { // users must set either ServerName or InsecureSkipVerify in the config.
			tlsConfig.ServerName = uri.Host
		}

		tlsConn := tls.Client(conn, tlsConfig)
		err = tlsConn.HandshakeContext(ctx)
		if err != nil {
			return nil, nil, err
		}
		conn = tlsConn
	}

	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}

	request := &http.Request{
		Method: http.MethodGet,
		URL:    &uri,
		Header: cHeaders,
	}

	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")

	if host := request.Header.Get("Host"); host != "" {
		// For client requests, Host optionally overrides the Host
		// header to send. If empty, the Request.Write method uses
		// the value of URL.Host. Host may contain an international
		// domain name.
		request.Host = host
		defer func() {
			request.Header.Set("Host", host) // recover for logging
		}()
	}
	request.Header.Del("Host")

	var secKey string
	if !v2rayHttpUpgrade {
		const nonceKeySize = 16
		// NOTE: bts does not escape.
		bts := make([]byte, nonceKeySize)
		if _, err = fastrand.Read(bts); err != nil {
			return nil, nil, fmt.Errorf("rand read error: %w", err)
		}
		secKey = base64.StdEncoding.EncodeToString(bts)
		request.Header.Set("Sec-WebSocket-Version", "13")
		request.Header.Set("Sec-WebSocket-Key", secKey)
	}

	done := proxy.SetupContextForConn(ctx, conn)
	defer done(&err)

	err = request.Write(conn)
	if err != nil {
		return nil, nil, err
	}
	bufferedConn := peek.NewBufferedConn(conn)

	response, err := http.ReadResponse(bufferedConn.Reader(), request)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode != http.StatusSwitchingProtocols ||
		!strings.EqualFold(response.Header.Get("Connection"), "upgrade") ||
		!strings.EqualFold(response.Header.Get("Upgrade"), "websocket") {
		return nil, response.Header, fmt.Errorf("unexpected status: %s", response.Status)
	}

	if v2rayHttpUpgrade {
		return bufferedConn, response.Header, nil
	}

	if false { // we might not check this for performance
		secAccept := response.Header.Get("Sec-Websocket-Accept")
		const acceptSize = 28 // base64.StdEncoding.EncodedLen(sha1.Size)
		if lenSecAccept := len(secAccept); lenSecAccept != acceptSize {
			return nil, response.Header, fmt.Errorf("unexpected Sec-Websocket-Accept length: %d", lenSecAccept)
		}
		if getSecAccept(secKey) != secAccept {
			return nil, response.Header, errors.New("unexpected Sec-Websocket-Accept")
		}
	}

	return NewWebsocketConn(conn, ws.StateClientSide, false), response.Header, nil
}

func getSecAccept(secKey string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	const nonceSize = 24 // base64.StdEncoding.EncodedLen(nonceKeySize)
	p := make([]byte, nonceSize+len(magic))
	copy(p[:nonceSize], secKey)
	copy(p[nonceSize:], magic)
	sum := sha1.Sum(p)
	return base64.StdEncoding.EncodeToString(sum[:])
}
