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

//go:embed splash
var splash string

//go:embed commit
var commit string

func main() {
	edges := []netip.AddrPort{}
	edgemode := flag.Bool("e", false, "Start as edge-node, you'll be alone to begin.")
	laddrstr := flag.String("l", "[::]:1618", "Listen address:port")
	bootstrap := flag.String("b", "", "Bootstrap address:port")
	dcap := flag.Uint("c", 100000, "Dat map capacity")
	difficulty := flag.Int("d", 2, "For set command. Number of leading zeros.")
	timeout := flag.Duration("t", 10*time.Second, "For get command. Timeout.")
	stat := flag.Bool("stat", false, "For get command. Output stats.")
	verbose := flag.Bool("v", false, "Verbose logging. Use grep.")
	flush := flag.Bool("flush", false, "Flush log buffer after each write. Not for production.")
	flag.Parse()
	if *edgemode {
		edges = []netip.AddrPort{}
	}
	if *bootstrap != "" {
		if strings.HasPrefix(*bootstrap, ":") {
			*bootstrap = "[::1]" + *bootstrap
		}
		addr, err := netip.ParseAddrPort(*bootstrap)
		if err != nil {
			exit(1, "failed to parse bootstrap addr -b=%q: %v", *bootstrap, err)
		}
		edges = []netip.AddrPort{addr}
	}
	laddr, err := net.ResolveUDPAddr("udp", *laddrstr)
	if err != nil {
		exit(1, "failed to resolve UDP listen address: %v", err)
	}
	var lch chan []byte
	if *verbose {
		lch = make(chan []byte, 100)
		go func(lch <-chan []byte) {
			var dlf *os.File
			if *verbose {
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
			}
		}(lch)
	}
	fmt.Printf("")
	d, err := godave.NewDave(&godave.Cfg{Listen: laddr, Edges: edges, DatCap: *dcap, Log: lch})
	if err != nil {
		exit(1, "failed to make dave: %v", err)
	}
	var action string
	if flag.NArg() > 0 {
		action = flag.Arg(0)
	}
	switch strings.ToLower(action) {
	case "version":
		fmt.Printf("%s commit %s\n", splash, commit)
		return
	case "set":
		if flag.NArg() < 2 {
			exit(1, "missing argument: set <VAL>")
		}
		set(d, []byte(flag.Arg(1)), *difficulty)
		return
	case "setf":
		if flag.NArg() < 2 {
			exit(1, "missing argument: setf <FILENAME>")
		}
		data, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			exit(2, "error reading file: %v", err)
		}
		set(d, data, *difficulty)
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
		fmt.Printf("%s commit %s\n", splash, commit)
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

func set(d *godave.Dave, val []byte, difficulty int) {
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
	dat := &godave.Dat{V: val, Ti: time.Now()}
	type sol struct{ work, salt []byte }
	solch := make(chan sol)
	ncpu := max(runtime.NumCPU()-2, 1)
	fmt.Printf("hashing with %d cores", ncpu)
	for n := 0; n < ncpu; n++ {
		fmt.Print(".")
		go func(v []byte, ti time.Time) {
			work, salt := godave.Work(v, godave.Ttb(ti), difficulty)
			solch <- sol{work, salt}
		}(dat.V, dat.Ti)
	}
	s := <-solch
	dat.W = s.work
	dat.S = s.salt
	done <- struct{}{}
	<-d.Set(*dat)
	fmt.Printf("\nWork: %x\nMass: %x\n", dat.W, godave.Mass(dat.W, dat.Ti))
}

func exit(code int, msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(code)
}
