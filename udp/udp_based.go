package udp

import (
	"fmt"
	"golang.org/x/exp/slices"
	"log"

	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback/ssaead"
)

type tunnel struct {
	address  string
	target   string
	reserved []byte

	ssTester *ssaead.Tester[string]
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
	return
}
