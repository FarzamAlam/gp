package main

import (
	"flag"
	"time"
)

func main() {
	timeout := flag.Duration("t", time.Second*100, "t seconds for timeout.")
	interval := flag.Duration("i", time.Second, "i seconds of interval.")
	count := flag.Int("c", -1, "")
	privileged := flag.Bool("privileged", false, "")

	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		return
	}
}
