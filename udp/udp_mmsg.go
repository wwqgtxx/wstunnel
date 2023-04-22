package udp

import (
	"fmt"
	"golang.org/x/exp/slices"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"log"
	"net"
	"sync"
	"time"

	"github.com/wwqgtxx/wstunnel/config"
	"github.com/wwqgtxx/wstunnel/fallback/ssaead"
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

type MmsgMapItem struct {
	net.Conn
	*ipv4.PacketConn
	sync.Mutex
}

type MmsgTunnel struct {
	connMap  sync.Map
	address  string
	target   string
	reserved []byte
	ssTester *ssaead.Tester[string]
}

func NewMmsgTunnel(udpConfig config.UdpConfig) Tunnel {
	t := &MmsgTunnel{
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
				v, _ := t.connMap.LoadOrStore(addr, &MmsgMapItem{})
				mapItem := v.(*MmsgMapItem)
				mapItem.Mutex.Lock()
				remoteConn := mapItem.Conn
				remotePacketConn := mapItem.PacketConn
				if remoteConn == nil || remotePacketConn == nil {
					target := t.target
					addition := ""
					if t.ssTester != nil {
						if ok, name, newTarget := t.ssTester.TestPacket(wMsgs[0].Buffers[0]); ok {
							addition = fmt.Sprintf("SS[%s]", name)
							target = newTarget
						}
					}
					log.Println("Dial", addition, "to", target, "for", addr)
					remoteConn, err = net.Dial("udp", t.target)
					if err != nil {
						mapItem.Mutex.Unlock()
						log.Println(err)
						return
					}
					log.Println("Associate from", addr, "to", remoteConn.RemoteAddr(), "by", remoteConn.LocalAddr())
					remotePacketConn = ipv4.NewPacketConn(remoteConn.(*net.UDPConn))
					mapItem.Conn = remoteConn
					mapItem.PacketConn = remotePacketConn
					go func() {
						rMsgs := ReadMsgsBufPool.Get().([]ipv4.Message)
						wMsgs := WriteMsgsBufPool.Get().([]ipv4.Message)
						defer func() {
							ReadMsgsBufPool.Put(rMsgs)
							WriteMsgsBufPool.Put(wMsgs)
						}()
						for {
							_ = remoteConn.SetReadDeadline(time.Now().Add(MaxUdpAge)) // set timeout
							n, err := remotePacketConn.ReadBatch(rMsgs, 0)
							if err != nil {
								t.connMap.Delete(addr)
								log.Println("Delete and close", remoteConn.LocalAddr(), "for", addr, "to", remoteConn.RemoteAddr(), "because", err)
								_ = remoteConn.Close()
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
							} else {
								var wN int
								wN, err = packetConn.WriteBatch(wMsgs[:wMsgsN], 0)
								if err == nil && wN != wMsgsN {
									log.Println("warning wN=", wN, "wMsgsN=", wMsgsN)
								}
							}
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

				for _, wMsg := range wMsgs[:wMsgsN] {
					buf := wMsg.Buffers[0]
					if len(t.reserved) > 0 && len(buf) > len(t.reserved) { // wireguard reserved
						copy(buf[1:], t.reserved)
					}
					wMsg.Addr = nil // set nil for connection-oriented udp from net.Dial
				}

				if wMsgsN == 1 { // maybe faster
					_, err = remoteConn.Write(wMsgs[0].Buffers[0])
				} else {
					var wN int
					wN, err = remotePacketConn.WriteBatch(wMsgs[:wMsgsN], 0)
					if err == nil && wN != wMsgsN {
						log.Println("warning wN=", wN, "wMsgsN=", wMsgsN)
					}
				}
				if err != nil {
					log.Println(err)
					return
				}
				_ = remoteConn.SetReadDeadline(time.Now().Add(MaxUdpAge)) // refresh timeout

			}()
		}

	}

}
