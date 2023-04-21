package udp

import (
	"golang.org/x/exp/slices"
	"golang.org/x/net/ipv4"
	"log"
	"net"
	"net/netip"
	"sync"

	"github.com/wwqgtxx/wstunnel/config"
	cache "github.com/wwqgtxx/wstunnel/utils/lrucache"
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

type StdNatItem struct {
	net.Conn
	*ipv4.PacketConn
	sync.Mutex
}

type StdTunnel struct {
	nat      *cache.LruCache[netip.AddrPort, *StdNatItem]
	address  string
	target   string
	reserved []byte
}

func NewStdTunnel(udpConfig config.UdpConfig) Tunnel {
	nat := cache.New[netip.AddrPort, *StdNatItem](
		cache.WithAge[netip.AddrPort, *StdNatItem](MaxUdpAge),
		cache.WithUpdateAgeOnGet[netip.AddrPort, *StdNatItem](),
		cache.WithEvict[netip.AddrPort, *StdNatItem](func(key netip.AddrPort, value *StdNatItem) {
			if conn := value.Conn; conn != nil {
				log.Println("Delete", conn.LocalAddr(), "for", key, "to", conn.RemoteAddr())
				_ = conn.Close()
			}
		}),
		cache.WithCreate[netip.AddrPort, *StdNatItem](func(key netip.AddrPort) *StdNatItem {
			return &StdNatItem{}
		}),
	)
	t := &StdTunnel{
		nat:      nat,
		address:  udpConfig.BindAddress,
		target:   udpConfig.TargetAddress,
		reserved: slices.Clone(udpConfig.Reserved),
	}
	return t
}

func (t *StdTunnel) Handle() {
	udpConn, err := ListenUdp("udp", t.address)
	if err != nil {
		log.Println(err)
		return
	}
	for {
		buf := BufPool.Get().([]byte)
		n, addr, err := udpConn.ReadFromUDPAddrPort(buf)
		if err != nil {
			BufPool.Put(buf)
			// TODO: handle close
			log.Println(err)
			continue
		}
		go func() {
			defer BufPool.Put(buf)
			var err error
			natItem, _ := t.nat.Get(addr)
			natItem.Mutex.Lock()
			remoteConn := natItem.Conn
			if remoteConn == nil {
				log.Println("Dial to", t.target, "for", addr)
				remoteConn, err = net.Dial("udp", t.target)
				if err != nil {
					natItem.Mutex.Unlock()
					log.Println(err)
					return
				}
				log.Println("Associate from", addr, "to", remoteConn.RemoteAddr(), "by", remoteConn.LocalAddr())
				natItem.Conn = remoteConn
				go func() {
					for {
						buf := BufPool.Get().([]byte)
						n, err := remoteConn.Read(buf)
						if err != nil {
							BufPool.Put(buf)
							t.nat.Delete(addr) // it will call remoteConn.Close() inside
							log.Println(err)
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
							t.nat.Delete(addr) // it will call remoteConn.Close() inside
							log.Println(err)
							return
						}
						t.nat.Get(addr) // refresh lru
					}
				}()
			}
			natItem.Mutex.Unlock()
			if len(t.reserved) > 0 && n > len(t.reserved) { // wireguard reserved
				copy(buf[1:], t.reserved)
			}
			_, err = remoteConn.Write(buf[:n])
			if err != nil {
				log.Println(err)
				return
			}
		}()

	}

}
