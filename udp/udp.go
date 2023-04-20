package udp

import (
	"golang.org/x/exp/slices"
	"log"
	"net"
	"net/netip"
	"sync"

	"github.com/wwqgtxx/wstunnel/config"
	cache "github.com/wwqgtxx/wstunnel/utils/lrucache"
)

const BufferSize = 16 * 1024

var BufPool = sync.Pool{New: func() any { return make([]byte, BufferSize) }}

func ListenUdp(address string) (*net.UDPConn, error) {
	pc, err := net.ListenPacket("udp", address)
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

type NatItem struct {
	net.Conn
	sync.Mutex
}

type Tunnel struct {
	nat      *cache.LruCache[netip.AddrPort, *NatItem]
	address  string
	target   string
	reserved []byte
}

func NewTunnel(udpConfig config.UdpConfig) *Tunnel {
	nat := cache.New[netip.AddrPort, *NatItem](
		cache.WithAge[netip.AddrPort, *NatItem](5*60),
		cache.WithUpdateAgeOnGet[netip.AddrPort, *NatItem](),
		cache.WithEvict[netip.AddrPort, *NatItem](func(key netip.AddrPort, value *NatItem) {
			if conn := value.Conn; conn != nil {
				log.Println("Delete", conn.LocalAddr(), "for", key, "to", conn.RemoteAddr())
				_ = conn.Close()
			}
		}),
		cache.WithCreate[netip.AddrPort, *NatItem](func(key netip.AddrPort) *NatItem {
			return &NatItem{}
		}),
	)
	t := &Tunnel{
		nat:      nat,
		address:  udpConfig.BindAddress,
		target:   udpConfig.TargetAddress,
		reserved: slices.Clone(udpConfig.Reserved),
	}
	return t
}

func (t *Tunnel) Handle() {
	udpConn, err := ListenUdp(t.address)
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
			conn := natItem.Conn
			if conn == nil {
				log.Println("Dial to", t.target, "for", addr)
				conn, err = net.Dial("udp", t.target)
				if err != nil {
					natItem.Mutex.Unlock()
					log.Println(err)
					return
				}
				log.Println("Associate from", addr, "to", conn.RemoteAddr(), "by", conn.LocalAddr())
				natItem.Conn = conn
				go func() {
					for {
						buf := BufPool.Get().([]byte)
						n, err := conn.Read(buf)
						if err != nil {
							BufPool.Put(buf)
							t.nat.Delete(addr) // it will call conn.Close() inside
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
							t.nat.Delete(addr) // it will call conn.Close() inside
							log.Println(err)
							return
						}
					}
				}()
			}
			natItem.Mutex.Unlock()
			if len(t.reserved) > 0 && n > len(t.reserved) { // wireguard reserved
				copy(buf[1:], t.reserved)
			}
			_, err = conn.Write(buf[:n])
			if err != nil {
				log.Println(err)
				return
			}
		}()

	}

}

var tunnels = make(map[string]*Tunnel)

func BuildUdp(udpConfig config.UdpConfig) {
	_, port, err := net.SplitHostPort(udpConfig.BindAddress)
	if err != nil {
		log.Println(err)
		return
	}
	tunnels[port] = NewTunnel(udpConfig)
}

func StartUdps() {
	for _, tunnel := range tunnels {
		go tunnel.Handle()
	}
}
