package udp

import (
	"fmt"
	"log"
	"slices"

	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback/quic"
	"github.com/wwqgtxx/wstunnel/fallback/ssaead"
)

type tunnel struct {
	address  string
	target   string
	reserved []byte

	ssTester   *ssaead.Tester[string]
	quicTester *quic.Tester[string]
}

func newTunnel(udpConfig config.UdpConfig) tunnel {
	t := tunnel{
		address:  udpConfig.BindAddress,
		target:   udpConfig.TargetAddress,
		reserved: slices.Clone(udpConfig.Reserved),
	}

	var err error
	if len(udpConfig.SSFallback) > 0 {
		t.ssTester = ssaead.NewTester[string]()
		for _, ssFallbackConfig := range udpConfig.SSFallback {
			err = t.ssTester.Add(
				ssFallbackConfig.Name,
				ssFallbackConfig.Method,
				ssFallbackConfig.Password,
				ssFallbackConfig.Address,
			)
			if err != nil {
				log.Println(err)
			}
		}
	}
	if len(udpConfig.QuicFallback) > 0 {
		t.quicTester = quic.NewTester[string]()
		for _, quicFallbackConfig := range udpConfig.QuicFallback {
			err = t.quicTester.Add(
				quicFallbackConfig.SNI,
				quicFallbackConfig.Address,
			)
			if err != nil {
				log.Println(err)
			}
		}
	}
	return t
}

func (t *tunnel) getTarget(packet []byte) (target, addition string) {
	target = t.target
	if len(packet) < 1 {
		return
	}
	if t.ssTester != nil {
		if ok, name, newTarget := t.ssTester.TestPacket(packet); ok {
			addition = fmt.Sprintf("SS[%s]", name)
			target = newTarget
			return
		}
	}
	if t.quicTester != nil {
		if ok, name, newTarget := t.quicTester.TestPacket(packet); ok {
			addition = fmt.Sprintf("Quic[%s]", name)
			target = newTarget
			return
		}
	}
	return
}
