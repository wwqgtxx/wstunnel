package udp

import (
	"golang.org/x/exp/slices"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"log"
	"net"
	"sync"

	"github.com/wwqgtxx/wstunnel/config"
	cache "github.com/wwqgtxx/wstunnel/utils/lrucache"
)

const (
	batchSize = 128
)

var ReadMsgsBufPool = sync.Pool{New: func() any {
	msgs := make([]ipv4.Message, batchSize)
	for i := range msgs {
		// preallocate the [][]byte
		msgs[i].Buffers = make([][]byte, 1)
		msgs[i].Buffers[0] = BufPool.Get().([]byte)
	}
	return msgs
}}

var WriteMsgsBufPool = sync.Pool{New: func() any {
	msgs := make([]ipv4.Message, batchSize)
	for i := range msgs {
		// preallocate the [][]byte
		msgs[i].Buffers = make([][]byte, 1)
	}
	return msgs
}}

// Contrary to what the naming suggests, the ipv{4,6}.Message is not dependent on the IP version.
// They're both just aliases for x/net/internal/socket.Message.
// This means we can use this struct to read from a socket that receives both IPv4 and IPv6 messages.
var _ ipv4.Message = ipv6.Message{}

type MmsgNatItem struct {
	net.Conn
	*ipv4.PacketConn
	sync.Mutex
}

type MmsgTunnel struct {
	nat      *cache.LruCache[string, *MmsgNatItem]
	address  string
	target   string
	reserved []byte
}

func NewMmsgTunnel(udpConfig config.UdpConfig) Tunnel {
	nat := cache.New[string, *MmsgNatItem](
		cache.WithAge[string, *MmsgNatItem](MaxUdpAge),
		cache.WithUpdateAgeOnGet[string, *MmsgNatItem](),
		cache.WithEvict[string, *MmsgNatItem](func(key string, value *MmsgNatItem) {
			if conn := value.Conn; conn != nil {
				log.Println("Delete", conn.LocalAddr(), "for", key, "to", conn.RemoteAddr())
				_ = conn.Close()
			}
		}),
		cache.WithCreate[string, *MmsgNatItem](func(key string) *MmsgNatItem {
			return &MmsgNatItem{}
		}),
	)
	t := &MmsgTunnel{
		nat:      nat,
		address:  udpConfig.BindAddress,
		target:   udpConfig.TargetAddress,
		reserved: slices.Clone(udpConfig.Reserved),
	}
	return t
}

func (t *MmsgTunnel) Handle() {
	udpConn, err := ListenUdp("udp", t.address)
	if err != nil {
		log.Println(err)
		return
	}
	packetConn := ipv4.NewPacketConn(udpConn)

	lastN := 0
	rMsgs := ReadMsgsBufPool.Get().([]ipv4.Message)
	defer func() {
		for i := 0; i < lastN; i++ {
			// replace buffers data buffers up to the packet that has been consumed during the last ReadBatch call
			rMsgs[i].Buffers[0] = BufPool.Get().([]byte)
		}
		ReadMsgsBufPool.Put(rMsgs)
	}()

	var visited [batchSize]bool

	for {
		for i := 0; i < lastN; i++ {
			// replace buffers data buffers up to the packet that has been consumed during the last ReadBatch call
			rMsgs[i].Buffers[0] = BufPool.Get().([]byte)
			visited[i] = false
		}
		n, err := packetConn.ReadBatch(rMsgs, 0)
		lastN = n
		if err != nil {
			// TODO: handle close
			log.Println(err)
			continue
		}
		for i := 0; i < n; i++ {
			if visited[i] {
				continue
			}
			visited[i] = true
			nAddr := rMsgs[i].Addr
			addr := nAddr.String()

			wMsgs := WriteMsgsBufPool.Get().([]ipv4.Message)
			wMsgs[0].Buffers[0] = rMsgs[i].Buffers[0][:rMsgs[i].N]
			wMsgsN := 1
			for j := i + 1; j < n; j++ {
				if visited[j] {
					continue
				}
				if addr != rMsgs[j].Addr.String() {
					continue
				}
				visited[j] = true

				wMsgs[wMsgsN].Buffers[0] = rMsgs[j].Buffers[0][:rMsgs[j].N]
				wMsgsN++
			}

			go func() {
				defer func() {
					for _, wMsg := range wMsgs[:wMsgsN] {
						buf := wMsg.Buffers[0]
						BufPool.Put(buf[:cap(buf)])
					}
					WriteMsgsBufPool.Put(wMsgs)
				}()
				natItem, _ := t.nat.Get(addr)
				natItem.Mutex.Lock()
				remoteConn := natItem.Conn
				remotePacketConn := natItem.PacketConn
				if remoteConn == nil || remotePacketConn == nil {
					log.Println("Dial to", t.target, "for", addr)
					remoteConn, err = net.Dial("udp", t.target)
					if err != nil {
						natItem.Mutex.Unlock()
						log.Println(err)
						return
					}
					log.Println("Associate from", addr, "to", remoteConn.RemoteAddr(), "by", remoteConn.LocalAddr())
					remotePacketConn = ipv4.NewPacketConn(remoteConn.(*net.UDPConn))
					natItem.Conn = remoteConn
					natItem.PacketConn = remotePacketConn
					go func() {
						rMsgs := ReadMsgsBufPool.Get().([]ipv4.Message)
						wMsgs := WriteMsgsBufPool.Get().([]ipv4.Message)
						defer func() {
							ReadMsgsBufPool.Put(rMsgs)
							WriteMsgsBufPool.Put(wMsgs)
						}()
						for {
							n, err := remotePacketConn.ReadBatch(rMsgs, 0)
							if err != nil {
								t.nat.Delete(addr) // it will call conn.Close() inside
								log.Println(err)
								return
							}
							for i := 0; i < n; i++ {
								buf := rMsgs[i].Buffers[0][:rMsgs[i].N]
								if len(t.reserved) > 0 && len(buf) > len(t.reserved) { // wireguard reserved
									for i := range t.reserved {
										buf[i+1] = 0
									}
								}
								wMsgs[i].Buffers[0] = buf
								wMsgs[i].Addr = nAddr
							}
							wMsgsN := n
							if wMsgsN == 1 { // maybe faster
								_, err = udpConn.WriteTo(wMsgs[0].Buffers[0], nAddr)
								if err != nil {
									t.nat.Delete(addr) // it will call conn.Close() inside
									log.Println(err)
									return
								}
							} else {
								var wN int
								wN, err = packetConn.WriteBatch(wMsgs[:wMsgsN], 0)
								if err != nil {
									t.nat.Delete(addr) // it will call conn.Close() inside
									log.Println(err)
									return
								}
								if wN != wMsgsN {
									log.Println("warning wN=", wN, "wMsgsN=", wMsgsN)
								}
							}
							t.nat.Get(addr) // refresh lru
						}
					}()
				}
				natItem.Mutex.Unlock()

				for _, wMsg := range wMsgs[:wMsgsN] {
					buf := wMsg.Buffers[0]
					if len(t.reserved) > 0 && len(buf) > len(t.reserved) { // wireguard reserved
						copy(buf[1:], t.reserved)
					}
					wMsg.Addr = nil // set nil for connection-oriented udp from net.Dial
				}

				if wMsgsN == 1 { // maybe faster
					_, err = remoteConn.Write(wMsgs[0].Buffers[0])
					if err != nil {
						log.Println(err)
						return
					}
				} else {
					var wN int
					wN, err = remotePacketConn.WriteBatch(wMsgs[:wMsgsN], 0)
					if err != nil {
						log.Println(err)
						return
					}
					if wN != wMsgsN {
						log.Println("warning wN=", wN, "wMsgsN=", wMsgsN)
					}
				}

			}()
		}

	}

}
