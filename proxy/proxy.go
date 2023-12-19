// Copyright 2017 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func init() {
	RegisterDialerType("http", func(proxyURL *url.URL, forwardDialer Dialer) (Dialer, error) {
		return &httpProxyDialer{proxyURL: proxyURL, forwardDialer: NewContextDialer(forwardDialer)}, nil
	})
}

func NewContextDialer(d Dialer) ContextDialer {
	if xd, ok := d.(ContextDialer); ok {
		return xd
	}
	return contextDialer{d}
}

type contextDialer struct {
	Dialer
}

func (d contextDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if ctx.Done() != nil {
		return dialContext(ctx, d, network, addr)
	}
	return d.Dial(network, addr)
}

func hostPortNoPort(u *url.URL) (hostPort, hostNoPort string) {
	hostPort = u.Host
	hostNoPort = u.Host
	if i := strings.LastIndex(u.Host, ":"); i > strings.LastIndex(u.Host, "]") {
		hostNoPort = hostNoPort[:i]
	} else {
		switch u.Scheme {
		case "wss":
			hostPort += ":443"
		case "https":
			hostPort += ":443"
		default:
			hostPort += ":80"
		}
	}
	return hostPort, hostNoPort
}

type httpProxyDialer struct {
	proxyURL      *url.URL
	forwardDialer ContextDialer
}

func (hpd *httpProxyDialer) Dial(network string, addr string) (conn net.Conn, err error) {
	return hpd.DialContext(context.Background(), network, addr)
}

func (hpd *httpProxyDialer) DialContext(ctx context.Context, network string, addr string) (conn net.Conn, err error) {
	hostPort, _ := hostPortNoPort(hpd.proxyURL)
	conn, err = hpd.forwardDialer.DialContext(ctx, network, hostPort)
	if err != nil {
		return
	}

	connectHeader := make(http.Header)
	if user := hpd.proxyURL.User; user != nil {
		proxyUser := user.Username()
		if proxyPassword, passwordSet := user.Password(); passwordSet {
			credential := base64.StdEncoding.EncodeToString([]byte(proxyUser + ":" + proxyPassword))
			connectHeader.Set("Proxy-Authorization", "Basic "+credential)
		}
	}

	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: connectHeader,
	}

	done := SetupContextForConn(ctx, conn)
	defer done(&err)

	if err = connectReq.Write(conn); err != nil {
		_ = conn.Close()
		conn = nil
		return
	}

	// Read response. It's OK to use and discard buffered reader here becaue
	// the remote server does not speak until spoken to.
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		_ = conn.Close()
		conn = nil
		return
	}

	if resp.StatusCode != 200 {
		_ = conn.Close()
		conn = nil
		f := strings.SplitN(resp.Status, " ", 2)
		err = errors.New(f[1])
		return
	}
	return
}
