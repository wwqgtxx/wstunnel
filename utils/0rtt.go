package utils

import (
	"encoding/base64"
	"net"
	"net/http"
	"strings"
)

var replacer = strings.NewReplacer("+", "-", "/", "_", "=", "")

func DecodeEd(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(replacer.Replace(s))
}

func DecodeXray0rtt(requestHeader http.Header) ([]byte, http.Header) {
	var edBuf []byte
	responseHeader := http.Header{}
	// read inHeader's `Sec-WebSocket-Protocol` for Xray's 0rtt ws
	if secProtocol := requestHeader.Get("Sec-WebSocket-Protocol"); len(secProtocol) > 0 {
		if buf, err := DecodeEd(secProtocol); err == nil { // sure could base64 decode
			edBuf = buf
			responseHeader.Set("Sec-WebSocket-Protocol", secProtocol)
		}
	}
	return edBuf, responseHeader
}

func EncodeEd(edBuf []byte) string {
	return base64.RawURLEncoding.EncodeToString(edBuf)
}

func EncodeXray0rtt(tcp net.Conn, ed uint32) (http.Header, []byte, error) {
	header := http.Header{}
	// Xray's 0rtt ws
	var edBuf []byte
	if ed > 0 {
		edBuf = make([]byte, ed)
		n, err := tcp.Read(edBuf)
		if err != nil {
			return nil, nil, err
		}
		edBuf = edBuf[:n]

		header.Set("Sec-WebSocket-Protocol", EncodeEd(edBuf))
	}
	return header, edBuf, nil
}
