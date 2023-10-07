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

func DecodeXray0rtt(requestHeader http.Header) []byte {
	// read inHeader's `Sec-WebSocket-Protocol` for Xray's 0rtt ws
	if secProtocol := requestHeader.Get("Sec-WebSocket-Protocol"); len(secProtocol) > 0 {
		if edBuf, err := DecodeEd(secProtocol); err == nil { // sure could base64 decode
			return edBuf
		}
	}
	return nil
}

func EncodeEd(edBuf []byte) string {
	return base64.RawURLEncoding.EncodeToString(edBuf)
}

func PrepareXray0rtt(tcp net.Conn, ed uint32) ([]byte, error) {
	// Xray's 0rtt ws
	var edBuf []byte
	if ed > 0 {
		edBuf = make([]byte, ed)
		n, err := tcp.Read(edBuf)
		if err != nil {
			return nil, err
		}
		edBuf = edBuf[:n]
	}
	return edBuf, nil
}
