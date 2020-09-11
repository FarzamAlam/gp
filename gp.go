package gp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	timeSliceLen     = 8
	trackerLen       = 8
	protocolICMP     = 1
	protocolIPv6ICMP = 58
)

var (
	ipv4Proto = map[string]string{"ip": "ip4:icmp", "udp": "udp4"}
	ipv6Proto = map[string]string{"ip": "ip6:ipv6-icmp", "udp": "udp6"}
)

// Pinger represents ICMP packet sender/receiver.
type Pinger struct {
	Count          int
	Debug          bool
	Interval       time.Duration
	Timeout        time.Duration
	PacketsSent    int
	PacketsRecieve int
	OnRecieve      func(*Packet)
	OnFinish       func(*Stats)
	Size           int
	Tracker        int64
	Source         string
	done           chan bool

	rtts     []time.Duration
	ipaddr   *net.IPAddr
	addr     string
	ipv4     bool
	size     int
	id       int
	sequence int
	network  string
}

// Packet represents a received and processed ICMP packet.
type Packet struct {
	Rtt      time.Duration
	IPAddr   *net.IPAddr
	Addr     string
	Nbytes   int
	Sequence int
	TTL      int
}

// Stats represents the statistics of running or finished pinger.
type Stats struct {
	PacketsRecieve int
	PacketsSent    int
	PacketsLoss    float64
	IPAddr         *net.IPAddr
	Addr           string
	Rtts           []time.Duration
	MinRtt         time.Duration
	MaxRtt         time.Duration
	AvgRtt         time.Duration
	StdDevRtt      time.Duration
}

type packet struct {
	data   []byte
	nbytes int
	ttl    int
}

// NewPinger returns a new Pinger.
func NewPinger(addr string) (*Pinger, error) {
	ipaddr, err := net.ResolveIPAddr("ip", addr)
	if err != nil {
		return nil, err
	}
	ipv4 := false
	if isIPv4(ipaddr.IP) {
		ipv4 = true
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &Pinger{
		ipaddr:   ipaddr,
		addr:     addr,
		Interval: time.Second,
		Timeout:  time.Second * 100,
		Count:    -1,
		id:       r.Intn(math.MaxInt16),
		network:  "udp",
		ipv4:     ipv4,
		size:     timeSliceLen,
		Tracker:  r.Int63n(math.MaxInt64),
		done:     make(chan bool),
	}, nil
}

// Run runs the pinger. This is a  blocking function that will exit when it's
// done. If Count or Interval are not specified, it will run continuously untill
// it is interrupted.
func (p *Pinger) Run() {
	var conn *icmp.PacketConn
	if p.ipv4 {
		conn = p.listen(ipv4Proto[p.network])
		if conn == nil {
			return
		}
		conn.IPv4PacketConn().SetControlMessage(ipv4.FlagTTL, true)
	} else {
		conn = p.listen(ipv6Proto[p.network])
		if conn == nil {
			return
		}
		conn.IPv6PacketConn().SetControlMessage(ipv6.FlagHopLimit, true)
	}
	defer conn.Close()
	defer p.finish()
	var wg sync.WaitGroup
	recieve := make(chan *packet, 5)
	defer close(recieve)
	wg.Add(1)
	go p.recieveICMP(conn, recieve, &wg)
	err := p.sendICMP(conn)
	if err != nil {
		log.Println("Error while calling sendICPM :", err.Error())
	}

	timeout := time.NewTicker(p.Timeout)
	defer timeout.Stop()

	interval := time.NewTicker(p.Interval)
	defer interval.Stop()

	for {
		select {
		case <-p.done:
			wg.Wait()
			return
		case <-timeout.C:
			close(p.done)
			wg.Wait()
			return
		case <-interval.C:
			if p.Count > 0 && p.PacketsSent >= p.Count {
				continue
			}

			err = p.sendICMP(conn)
			if err != nil {
				fmt.Println("FATAL : ", err.Error())
			}
		case r := <-recieve:
			err := p.processPacket(r)
			if err != nil {
				fmt.Println("FATAL : ", err.Error())
			}
			if p.Count > 0 && p.PacketsRecieve >= p.Count {
				close(p.done)
				wg.Wait()
				return
			}
		}
	}
}
func (p *Pinger) recieveICMP(conn *icmp.PacketConn, recieve chan<- *packet, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-p.done:
			return
		default:
			data := make([]byte, 512)
			conn.SetReadDeadline(time.Now().Add(time.Millisecond * 1000))
			var n, ttl int
			var err error
			if p.ipv4 {
				var cm *ipv4.ControlMessage
				n, cm, _, err = conn.IPv4PacketConn().ReadFrom(data)
				if cm != nil {
					ttl = cm.TTL
				}
			} else {
				var cm *ipv6.ControlMessage
				n, cm, _, err = conn.IPv6PacketConn().ReadFrom(data)
				if cm != nil {
					ttl = cm.HopLimit
				}
			}
			if err != nil {
				if neterr, ok := err.(*net.OpError); ok {
					if neterr.Timeout() {
						// Read timeout
						continue
					} else {
						close(p.done)
						return
					}
				}
			}
			recieve <- &packet{data: data, nbytes: n, ttl: ttl}
		}
	}
}

func (p *Pinger) sendICMP(conn *icmp.PacketConn) error {
	var typ icmp.Type
	if p.ipv4 {
		typ = ipv4.ICMPTypeEcho
	} else {
		typ = ipv6.ICMPTypeEchoRequest
	}
	var dest net.Addr = p.ipaddr
	if p.network == "udp" {
		dest = &net.UDPAddr{IP: p.ipaddr.IP, Zone: p.ipaddr.Zone}
	}
	t := append(timeToBytes(time.Now()), intToBytes(p.Tracker)...)

	if remainSize := p.Size - timeSliceLen - trackerLen; remainSize > 0 {
		t = append(t, bytes.Repeat([]byte{1}, remainSize)...)
	}

	body := &icmp.Echo{
		ID:   p.id,
		Seq:  p.sequence,
		Data: t,
	}
	msg := &icmp.Message{
		Type: typ,
		Code: 0,
		Body: body,
	}
	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return err
	}
	for {
		if _, err := conn.WriteTo(msgBytes, dest); err != nil {
			if neterr, ok := err.(*net.OpError); ok {
				if neterr.Err == syscall.ENOBUFS {
					continue
				}
			}
		}
		p.PacketsSent++
		p.sequence++
		break
	}
	return nil
}

func (p *Pinger) processPacket(packet *packet) error {
	recievedAt := time.Now()
	var protocol int

	if p.ipv4 {
		protocol = protocolICMP
	} else {
		protocol = protocolIPv6ICMP
	}
	var m *icmp.Message
	var err error

	if m, err = icmp.ParseMessage(protocol, packet.data); err != nil {
		return fmt.Errorf("Error while parsing icmp message : %s ", err.Error())
	}

	if m.Type != ipv4.ICMPTypeEchoReply && m.Type != ipv6.ICMPTypeEchoRequest {
		return nil
	}

	outputPacket := &Packet{
		Nbytes: packet.nbytes,
		IPAddr: p.ipaddr,
		Addr:   p.addr,
		TTL:    packet.ttl,
	}
	switch pkt := m.Body.(type) {
	case *icmp.Echo:
		if p.network == "ip" {
			if pkt.ID != p.id {
				return nil
			}
		}
		if len(pkt.Data) < timeSliceLen+trackerLen {
			return fmt.Errorf("Insufficient data receive. Got %d %v", len(pkt.Data), pkt.Data)
		}
		tracker := bytesToInt(pkt.Data[timeSliceLen:])
		timestamp := bytesToTime(pkt.Data[:timeSliceLen])
		if tracker != p.Tracker {
			return nil
		}
		outputPacket.Rtt = recievedAt.Sub(timestamp)
		outputPacket.Sequence = pkt.Seq
		p.PacketsRecieve++
	default:
		return fmt.Errorf("Invalid ICMP echo reply. Type : %T, %v", pkt, pkt)

	}
	p.rtts = append(p.rtts, outputPacket.Rtt)
	handler := p.OnRecieve
	if handler != nil {
		handler(outputPacket)
	}
	return nil
}
func (p *Pinger) SetPrivileged(privileged bool) {
	if privileged {
		p.network = "ip"
	} else {
		p.network = "udp"
	}
}

// GenerateStats returns the statistics of the pinger. This can be run while
// Pinger is runnig or after it is finished.
// OnFinish calls this func to get its finished stats.
func (p *Pinger) GenerateStats() *Stats {
	loss := float64(p.PacketsSent-p.PacketsRecieve) / float64(p.PacketsSent) * 100
	var min, max, total time.Duration

	if len(p.rtts) > 0 {
		min = p.rtts[0]
		max = p.rtts[0]
	}
	for _, rtt := range p.rtts {
		if rtt < min {
			min = rtt
		}
		if rtt > max {
			max = rtt
		}
		total += rtt
	}
	s := Stats{
		PacketsSent:    p.PacketsSent,
		PacketsRecieve: p.PacketsRecieve,
		PacketsLoss:    loss,
		Rtts:           p.rtts,
		Addr:           p.addr,
		IPAddr:         p.ipaddr,
		MaxRtt:         max,
		MinRtt:         min,
	}
	if len(p.rtts) > 0 {
		s.AvgRtt = total / time.Duration(len(p.rtts))
		var sumSquares time.Duration
		for _, rtt := range p.rtts {
			sumSquares += (rtt - s.AvgRtt) * (rtt - s.AvgRtt)
		}
		s.StdDevRtt = time.Duration(math.Sqrt(float64(sumSquares / time.Duration(len(p.rtts)))))
	}
	return &s
}

// finish method is called after the pinger stops.
func (p *Pinger) finish() {
	handler := p.OnFinish
	if handler != nil {
		s := p.GenerateStats()
		handler(s)
	}
}

func isIPv4(ip net.IP) bool {
	return net.IPv4len == len(ip.To4())
}

func (p *Pinger) Stop() {
	close(p.done)
}

// Addr returns the hostname or string IP address.
func (p *Pinger) Addr() string {
	return p.addr
}

// Returns the IP address of target host.
func (p *Pinger) IPAddr() *net.IPAddr {
	return p.ipaddr
}
func (p *Pinger) listen(netProto string) *icmp.PacketConn {
	conn, err := icmp.ListenPacket(netProto, p.Source)
	if err != nil {
		log.Printf("Error listening for ICMP Packets: %s\n", err.Error())
		close(p.done)
		return nil
	}
	return conn
}

// timeToBytes converts time to byte slice.
func timeToBytes(t time.Time) []byte {
	nsec := t.UnixNano()
	b := make([]byte, 8)

	for i := uint8(0); i < 8; i++ {
		b[i] = byte((nsec >> ((7 - i) * 8)) & 0xff)
	}
	return b
}

// bytesToTime converts byte slice to time.
func bytesToTime(b []byte) time.Time {
	var nsec int64
	for i := uint8(0); i < 8; i++ {
		nsec += int64(b[i]) << ((7 - i) * 8)
	}
	return time.Unix(nsec/1000000000, nsec%1000000000)
}

// intToBytes converts int64 to byte slice.
func intToBytes(tracker int64) []byte {
	b := make([]byte, 8)

	binary.BigEndian.PutUint64(b, uint64(tracker))
	return b
}

// bytesToInt converts byte slice to int64.
func bytesToInt(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(b))
}
