package utils

// modify from https://github.com/inetaf/tcpproxy/blob/91f861402626c6ba93eaa57ee257109c4f07bd00/sni.go#L80-L115

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"time"

	"github.com/wwqgtxx/wstunnel/peek"
)

// ClientHelloServerName returns the SNI server name inside the TLS ClientHello,
// without consuming any bytes from br.
// On any error, the empty string is returned.
func ClientHelloServerName(peeker peek.Peeker) (sni string) {
	const recordHeaderLen = 5
	hdr, err := peeker.Peek(recordHeaderLen)
	if err != nil {
		return ""
	}
	const recordTypeHandshake = 0x16
	if hdr[0] != recordTypeHandshake {
		return "" // Not TLS.
	}
	recLen := int(hdr[3])<<8 | int(hdr[4]) // ignoring version in hdr[1:3]
	helloBytes, err := peeker.Peek(recordHeaderLen + recLen)
	if err != nil {
		return ""
	}
	_ = tls.Server(sniSniffConn{r: bytes.NewReader(helloBytes)}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni = hello.ServerName
			return nil, nil
		},
	}).Handshake()
	return
}

// sniSniffConn is a net.Conn that reads from r, fails on Writes or otherwise,
type sniSniffConn struct {
	r io.Reader
}

func (c sniSniffConn) Read(p []byte) (int, error)         { return c.r.Read(p) }
func (c sniSniffConn) Write(p []byte) (int, error)        { return 0, io.EOF }
func (c sniSniffConn) Close() error                       { return nil }
func (c sniSniffConn) LocalAddr() net.Addr                { return nil }
func (c sniSniffConn) RemoteAddr() net.Addr               { return nil }
func (c sniSniffConn) SetDeadline(t time.Time) error      { return nil }
func (c sniSniffConn) SetReadDeadline(t time.Time) error  { return nil }
func (c sniSniffConn) SetWriteDeadline(t time.Time) error { return nil }
