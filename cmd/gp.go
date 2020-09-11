package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/farzamalam/gp"
)

func main() {
	timeout := flag.Duration("t", time.Second*100, "t seconds for timeout.")
	interval := flag.Duration("i", time.Second, "i seconds of interval.")
	count := flag.Int("c", -1, "")
	//privileged := flag.Bool("privileged", false, "")

	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		return
	}
	host := flag.Arg(0)
	pinger, err := gp.NewPinger(host)

	if err != nil {
		fmt.Printf("Error: %s\n", err)
	}
	// listen for CTRL+C signal.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			pinger.Stop()
		}
	}()

	pinger.OnRecieve = func(pkt *gp.Packet) {
		fmt.Printf("%d bytes from %s: icmp_seq=%d time=%v ttl=%v\n", pkt.Nbytes, pkt.IPAddr, pkt.Sequence, pkt.Rtt, pkt.TTL)
	}
	pinger.OnFinish = func(stats *gp.Stats) {
		fmt.Printf("\n----- %s ping statistics ----\n", stats.Addr)
		fmt.Printf("%d packets transmitted, %d packets received, %v%% packet loss\n", stats.PacketsSent, stats.PacketsRecieve, stats.PacketsLoss)
		fmt.Printf("round-trip min/avg/max/stddev = %v/%v/%v/%v\n", stats.MinRtt, stats.AvgRtt, stats.MaxRtt, stats.StdDevRtt)
	}
	pinger.Count = *count
	pinger.Interval = *interval
	pinger.Timeout = *timeout
	pinger.SetPrivileged(true)

	fmt.Printf("PING %s (%s):\n", pinger.Addr(), pinger.IPAddr())
	pinger.Run()
}
