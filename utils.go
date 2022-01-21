package main

import (
	"encoding/base64"
	"net"
	"net/http"
	"strings"
)

var replacer = strings.NewReplacer("+", "-", "/", "_", "=", "")

func decodeEd(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(replacer.Replace(s))
}

func decodeXray0rtt(requestHeader http.Header) ([]byte, http.Header) {
	var edBuf []byte
	responseHeader := http.Header{}
	// read inHeader's `Sec-WebSocket-Protocol` for Xray's 0rtt ws
	if secProtocol := requestHeader.Get("Sec-WebSocket-Protocol"); len(secProtocol) > 0 {
		if buf, err := decodeEd(secProtocol); err == nil { // sure could base64 decode
			edBuf = buf
			responseHeader.Set("Sec-WebSocket-Protocol", secProtocol)
		}
	}
	return edBuf, responseHeader
}

func encodeEd(edBuf []byte) string {
	return base64.RawURLEncoding.EncodeToString(edBuf)
}

func encodeXray0rtt(tcp net.Conn, c *wsClientImpl) (http.Header, []byte, error) {
	header := http.Header{}
	// Xray's 0rtt ws
	var edBuf []byte
	if c.ed > 0 {
		edBuf = make([]byte, c.ed)
		n, err := tcp.Read(edBuf)
		if err != nil {
			return nil, nil, err
		}
		edBuf = edBuf[:n]

		header.Set("Sec-WebSocket-Protocol", encodeEd(edBuf))
	}
	return header, edBuf, nil
}
