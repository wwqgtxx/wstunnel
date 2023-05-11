package udp

import (
	"fmt"
	"golang.org/x/exp/slices"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"sync"
	"time"

	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback/ssaead"
)

const BufferSize = 16 * 1024

var BufPool = sync.Pool{New: func() any { return make([]byte, BufferSize) }}

func ListenUdp(network, address string) (*net.UDPConn, error) {
	pc, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

type StdMapItem struct {
	net.Conn
	*ipv4.PacketConn
	sync.Mutex
}

type StdTunnel struct {
	connMap  sync.Map
	address  string
	target   string
	reserved []byte
	ssTester *ssaead.Tester[string]
}

func NewStdTunnel(udpConfig config.UdpConfig) Tunnel {
	t := &StdTunnel{
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

func (t *StdTunnel) Handle() {
	udpConn, err := ListenUdp("udp", t.address)
	if err != nil {
		log.Println(err)
		return
	}
	enhanceUDPConn := NewEnhancePacketConn(udpConn)
	for {
		data, put, addr, err := enhanceUDPConn.WaitReadFrom()
		if err != nil {
			// TODO: handle close
			log.Println(err)
			continue
		}
		go func() {
			defer put()
			var err error
			v, _ := t.connMap.LoadOrStore(addr, &StdMapItem{})
			mapItem := v.(*StdMapItem)
			mapItem.Mutex.Lock()
			remoteConn := mapItem.Conn
			if remoteConn == nil {
				target := t.target
				addition := ""
				if t.ssTester != nil {
					if ok, name, newTarget := t.ssTester.TestPacket(data); ok {
						addition = fmt.Sprintf("SS[%s]", name)
						target = newTarget
					}
				}
				log.Println("Dial", addition, "to", target, "for", addr)
				remoteConn, err = net.Dial("udp", target)
				if err != nil {
					mapItem.Mutex.Unlock()
					log.Println(err)
					return
				}
				log.Println("Associate", addition, "from", addr, "to", remoteConn.RemoteAddr(), "by", remoteConn.LocalAddr())
				mapItem.Conn = remoteConn
				go func() {
					for {
						buf := BufPool.Get().([]byte)
						_ = remoteConn.SetReadDeadline(time.Now().Add(MaxUdpAge)) // set timeout
						n, err := remoteConn.Read(buf)
						if err != nil {
							BufPool.Put(buf)
							t.connMap.Delete(addr)
							log.Println("Delete and close", remoteConn.LocalAddr(), "for", addr, "to", remoteConn.RemoteAddr(), "because", err)
							_ = remoteConn.Close()
							return
						}
						if len(t.reserved) > 0 && n > len(t.reserved) { // wireguard reserved
							for i := range t.reserved {
								buf[i+1] = 0
							}
						}
						_, err = udpConn.WriteToUDPAddrPort(buf[:n], addr)
						BufPool.Put(buf)
						if err != nil {
							t.connMap.Delete(addr)
							log.Println("Delete and close", remoteConn.LocalAddr(), "for", addr, "to", remoteConn.RemoteAddr(), "because", err)
							_ = remoteConn.Close()
							return
						}
					}
				}()
			}
			mapItem.Mutex.Unlock()
			if len(t.reserved) > 0 && len(data) > len(t.reserved) { // wireguard reserved
				copy(data[1:], t.reserved)
			}
			_, err = remoteConn.Write(data)
			if err != nil {
				log.Println(err)
				return
			}
			_ = remoteConn.SetReadDeadline(time.Now().Add(MaxUdpAge)) // refresh timeout
		}()

	}

}
