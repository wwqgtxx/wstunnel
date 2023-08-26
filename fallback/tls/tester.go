package tls

// modify from https://github.com/inetaf/tcpproxy/blob/91f861402626c6ba93eaa57ee257109c4f07bd00/sni.go#L80-L115

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"time"

	"github.com/wwqgtxx/wstunnel/peek"
)

type Pair[T any] struct {
	Name string
	Val  T
}

type Tester[T any] struct {
	Map map[string]T
}

func NewTester[T any]() *Tester[T] {
	return &Tester[T]{Map: make(map[string]T)}
}

var (
	StartBytes = [3]byte{0x16, 0x03, 0x01} // TLS1.0 Handshake (works on TLS1.0, 1.1, 1.2, 1.3)
)

func (t *Tester[T]) Add(name string, val T) (err error) {
	t.Map[name] = val
	return
}

func (t *Tester[T]) Test(peeker peek.Peeker, cb func(name string, val T)) (bool, error) {
	const recordHeaderLen = 5
	hdr, err := peeker.Peek(recordHeaderLen)
	if err != nil {
		return false, err
	}
	if hdr[0] != StartBytes[0] || hdr[1] != StartBytes[1] || hdr[2] != StartBytes[2] {
		return false, nil
	}
	recLen := int(hdr[3])<<8 | int(hdr[4]) // ignoring version in hdr[1:3]
	helloBytes, err := peeker.Peek(recordHeaderLen + recLen)
	if err != nil {
		return false, nil
	}

	sni := ExtractSniFromBytes(helloBytes)

	if val, ok := t.Map[sni]; ok {
		cb(sni, val)
		return true, nil
	}
	if val, ok := t.Map[""]; ok {
		cb(sni, val)
		return true, nil
	}
	return false, nil
}

func ExtractSniFromBytes(helloBytes []byte) (sni string) {
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
