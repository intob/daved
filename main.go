// Public domain.
package main

import (
	"bufio"
	_ "embed"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/intob/godave"
	"github.com/intob/jfmt"
)

//go:embed commit
var commit string

func main() {
	laddrstr := flag.String("l", "[::]:1618", "Listen address:port")
	edge := flag.String("e", "", "Edge bootstrap address:port")
	epoch := flag.Duration("epoch", 20*time.Microsecond, "Base cycle period. Reduce to increase bandwidth usage.")
	dcap := flag.Uint("dcap", 100000, "Dat map capacity")
	fcap := flag.Uint("fcap", 10000, "Cuckoo filter capacity. 10K (default) or 100K should be good ;)")
	pull := flag.Uint64("pull", 0, "Interval between pulling a random dat from a random peer (optional anonymity)")
	prune := flag.Uint64("prune", 50000, "Interval between refreshing dat & peer maps")
	difficulty := flag.Int("d", 2, "For set command. Number of leading zeros.")
	rounds := flag.Int("rounds", 9, "For set command. Number of times to repeat sending dat.")
	npeer := flag.Int("npeer", 64, "Number of peer messages to collect before sending dat.")
	ntest := flag.Int("ntest", 1, "For set command. Repeat work & send n times. For testing.")
	timeout := flag.Duration("t", 3*time.Second, "For get command. Timeout.")
	retry := flag.Duration("r", 100*time.Millisecond, "For get command. Interval between sending GET messages.")
	stat := flag.Bool("s", false, "For get command. Output performance measurements.")
	verbose := flag.Bool("v", false, "Verbose logging. Use grep.")
	flush := flag.Bool("f", false, "Flush log buffer after each write.")
	flag.Parse()
	laddr, err := net.ResolveUDPAddr("udp", *laddrstr)
	if err != nil {
		exit(1, "failed to resolve UDP listen address: %v", err)
	}
	var lch chan []byte
	if *verbose {
		lch = make(chan []byte, 100)
		go func(lch <-chan []byte) {
			var dlf *os.File
			dlf = os.Stdout
			defer dlf.Close()
			dlw := bufio.NewWriter(dlf)
			fl := *flush
			for l := range lch {
				dlw.Write(l)
				if fl {
					dlw.Flush()
				}
			}
		}(lch)
	}
	edges := []netip.AddrPort{}
	if *edge != "" {
		if strings.HasPrefix(*edge, ":") {
			*edge = "[::1]" + *edge
		}
		addr, err := netip.ParseAddrPort(*edge)
		if err != nil {
			exit(1, "failed to parse edge bootstrap addr -e=%q: %v", edge, err)
		}
		edges = append(edges, addr)
	}
	fmt.Printf("listening on %s, edges: %+v\n", laddr.String(), edges)
	d, err := godave.NewDave(&godave.Cfg{LstnAddr: laddr, Edges: edges, Epoch: *epoch, DatCap: *dcap, FilterCap: *fcap, Pull: *pull, Prune: *prune, Log: lch})
	if err != nil {
		exit(1, "failed to make dave: %v", err)
	}
	var action string
	if flag.NArg() > 0 {
		action = flag.Arg(0)
	}
	switch action {
	case "version":
		fmt.Printf("commit %s\n", commit)
		return
	case "set":
		if flag.NArg() < 2 {
			exit(1, "missing argument: set <VAL>")
		}
		set(d, []byte(flag.Arg(1)), *difficulty, *rounds, *npeer, *ntest)
		return
	case "setf":
		if flag.NArg() < 2 {
			exit(1, "missing argument: setf <FILENAME>")
		}
		data, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			exit(2, "error reading file: %v", err)
		}
		set(d, data, *difficulty, *rounds, *npeer, *ntest)
		return
	case "get":
		if flag.NArg() < 2 {
			exit(1, "correct usage is get <WORK>")
		}
		work, err := hex.DecodeString(flag.Arg(1))
		if err != nil {
			exit(1, "invalid input <WORK>: %v", err)
		}
		tstart := time.Now()
		var found bool
		for dat := range d.Get(work, *timeout, *retry) {
			found = true
			fmt.Println(string(dat.V))
		}
		if !found {
			exit(1, "dat not found")
		}
		if *stat {
			fmt.Printf("t: %s\n", time.Since(tstart))
		}
		return
	}
	if *verbose {
		<-make(chan struct{})
	} else {
		fmt.Printf("commit %s\n", commit)
		var i uint64
		ts := time.Now()
		tick := time.NewTicker(time.Second)
		for {
			select {
			case <-d.Recv:
				i++
			case <-tick.C:
				fmt.Printf("\rhandled %s dats in %s\033[0K", jfmt.FmtCount64(i), jfmt.FmtDuration(time.Since(ts)))
			}
		}
	}
}

func set(d *godave.Dave, val []byte, difficulty, rounds, npeer, ntest int) {
	done := make(chan struct{})
	go func() {
		ti := time.NewTicker(time.Second)
		t := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ti.C:
				fmt.Printf("\rworking for %s\033[0K", jfmt.FmtDuration(time.Since(t)))
			}
		}
	}()
	type chal struct {
		v  []byte
		ti time.Time
	}
	type sol struct{ work, salt []byte }
	chalch := make(chan chal, 1)
	solch := make(chan sol, 1)
	ncpu := max(runtime.NumCPU()-1, 1)
	fmt.Printf("hashing with %d cores", ncpu)
	for nroutine := 0; nroutine < ncpu; nroutine++ {
		go func(chalch <-chan chal) {
			for c := range chalch {
				work, salt := godave.Work(c.v, godave.Ttb(c.ti), difficulty)
				solch <- sol{work, salt}
			}
		}(chalch)
	}
	for i := 0; i < ntest; i++ {
		dat := &godave.Dat{V: val, Ti: time.Now()}
		chalch <- chal{dat.V, dat.Ti}
		s := <-solch
		dat.W = s.work
		dat.S = s.salt
		fmt.Printf("\nWork: %x\nMass: %x\n", dat.W, godave.Mass(dat.W, dat.Ti))
		<-d.Set(*dat, rounds, npeer)
	}
	done <- struct{}{}
}

func exit(code int, msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(code)
}
