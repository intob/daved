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
	edges := []netip.AddrPort{
		netip.MustParseAddrPort("54.170.214.154:1618"),
		netip.MustParseAddrPort("3.249.184.30:1618"),
		netip.MustParseAddrPort("18.200.244.108:1618"),
	}
	edgemode := flag.Bool("e", false, "Start as edge-node, you'll be alone to begin.")
	lap := flag.String("l", "[::]:1618", "Listen address:port")
	edge := flag.String("b", "", "Bootstrap address:port")
	dcap := flag.Uint("dc", 100000, "Dat map capacity")
	npeer := flag.Int("n", 50*godave.NPEER, "For set command. Number of peers to collect before sending.")
	difficulty := flag.Int("d", 2, "For set command. Number of leading zeros.")
	timeout := flag.Duration("t", 10*time.Second, "For get command. Timeout.")
	stat := flag.Bool("stat", false, "For get command. Output stats.")
	verbose := flag.Bool("v", false, "Verbose logging. Use grep.")
	flush := flag.Bool("flush", false, "Flush log buffer after each write. Not for production.")
	flag.Parse()
	if *edgemode {
		edges = []netip.AddrPort{}
	}
	if *edge != "" {
		if strings.HasPrefix(*edge, ":") {
			*edge = "[::1]" + *edge
		}
		addr, err := netip.ParseAddrPort(*edge)
		if err != nil {
			exit(1, "failed to parse -p=%q: %v", *edge, err)
		}
		edges = []netip.AddrPort{addr}
	}
	laddr, err := net.ResolveUDPAddr("udp", *lap)
	if err != nil {
		exit(1, "failed to resolve UDP address: %v", err)
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
	} else {
	}
	d, err := godave.NewDave(&godave.Cfg{
		Listen: laddr,
		Edges:  edges,
		DatCap: *dcap,
		Log:    lch})
	if err != nil {
		exit(1, "failed to make dave: %v", err)
	}
	var action string
	if flag.NArg() > 0 {
		action = flag.Arg(0)
	}
	switch strings.ToLower(action) {
	case "version":
		fmt.Printf("%scommit %s\n", splash, commit)
		return
	case "set":
		if flag.NArg() < 2 {
			exit(1, "missing argument: set <VAL>")
		}
		set(d, []byte(flag.Arg(1)), *difficulty, *npeer)
		return
	case "setfile":
		if flag.NArg() < 2 {
			exit(1, "missing argument: setfile <FILENAME>")
		}
		data, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			exit(2, "error reading file: %v", err)
		}
		set(d, data, *difficulty, *npeer)
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
		fmt.Printf("%scommit %s\n", splash, commit)
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

func set(d *godave.Dave, val []byte, difficulty, npeer int) {
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
