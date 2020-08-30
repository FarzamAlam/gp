package pg

import (
	"math"
	"math/rand"
	"net"
	"time"
)

const (
	timeSliceLen     = 8
	trackerLen       = 8
	protocolICMP     = 1
	protocolIPv6ICMP = 58
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

func isIPv4(ip net.IP) bool {
	return net.IPv4len == len(ip.To4())
}
