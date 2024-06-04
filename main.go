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
	laddrstr := flag.String("l", "[::]:127", "Listen address:port")
	edge := flag.String("e", "", "Edge bootstrap address:port")
	difficulty := flag.Uint("d", 16, "For set command. Number of leading zero bits.")
	test := flag.Bool("t", false, "Test mode. Allows unlimited ports per IP.")
	verbose := flag.Bool("v", false, "Verbose logging. Use grep.")
	flush := flag.Bool("f", false, "Flush log buffer after each write.")
	epoch := flag.Duration("epoch", 10*time.Microsecond, "Base cycle period. Reduce to increase bandwidth usage.")
	backup := flag.String("backup", "", "Backup file. Dats and peers will be written periodically. Set to enable.")
	scap := flag.Int("scap", 10000, "Dat map shard capacity. Each shard corresponds to a difficulty level.")
	fcap := flag.Uint("fcap", 1000, "Cuckoo filter capacity. 1K (default) or 10K should be good ;)")
	prune := flag.Int("prune", 50000, "Interval in epochs between refreshing dat & peer maps & writing backup.")
	rounds := flag.Int("rounds", 9, "For set command. Number of times to repeat sending dat.")
	ntest := flag.Int("ntest", 1, "For set command. Repeat work & send n times. For testing.")
	timeout := flag.Duration("timeout", 5*time.Second, "For get command. Timeout.")
	stat := flag.Bool("stat", false, "For get command. Output performance measurements.")
	flag.Parse()
	laddr, err := net.ResolveUDPAddr("udp", *laddrstr)
	if err != nil {
		exit(1, "failed to resolve UDP listen address: %v", err)
	}
	var lch chan []byte
	if *verbose {
		lch = make(chan []byte, 100)
		go func(lch <-chan []byte) {
			dlw := bufio.NewWriter(os.Stdout)
			for l := range lch {
				fmt.Fprint(dlw, string(l))
				if *flush {
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
	d, err := godave.NewDave(&godave.Cfg{
		LstnAddr:    laddr,
		Edges:       edges,
		Epoch:       *epoch,
		Prune:       *prune,
		ShardCap:    *scap,
		FilterCap:   *fcap,
		Test:        *test,
		Log:         lch,
		BackupFname: *backup,
	})
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
		set(d, []byte(flag.Arg(1)), uint8(*difficulty), *rounds, *ntest, *epoch)
		return
	case "setf":
		if flag.NArg() < 2 {
			exit(1, "missing argument: setf <FILENAME>")
		}
		data, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			exit(2, "error reading file: %v", err)
		}
		set(d, data, uint8(*difficulty), *rounds, *ntest, *epoch)
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
		for dat := range d.Get(work, *timeout) {
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

func set(d *godave.Dave, val []byte, difficulty uint8, rounds, ntest int, epoch time.Duration) {
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
		<-d.Set(*dat, rounds)
		if ntest > 1 {
			time.Sleep(epoch)
		}
	}
	done <- struct{}{}
}

func exit(code int, msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(code)
}
