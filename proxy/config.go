package proxy

import (
	"log"
	"net"
	"net/url"
)

func FromProxyString(proxy string) (ContextDialer, string) {
	proxyUrl, proxyStr := parseProxy(proxy)
	dialer := getDialer(proxyUrl)
	return dialer, proxyStr
}

func parseProxy(proxyString string) (proxyUrl *url.URL, proxyStr string) {
	if len(proxyString) > 0 {
		u, err := url.Parse(proxyString)
		if err != nil {
			log.Println(err)
		}
		proxyUrl = u

		ru := *u
		ru.User = nil
		proxyStr = ru.String()
	}
	return
}

func getDialer(proxyUrl *url.URL) ContextDialer {
	tcpDialer := &net.Dialer{}

	proxyDialer := FromEnvironment()
	if proxyUrl != nil {
		dialer, err := FromURL(proxyUrl, tcpDialer)
		if err != nil {
			log.Println(err)
		} else {
			proxyDialer = dialer
		}
	}
	if proxyDialer != Direct {
		return NewContextDialer(proxyDialer)
	} else {
		return tcpDialer
	}
}
