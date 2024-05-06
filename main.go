package main

import (
	"bufio"
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
	"github.com/intob/godave/dave"
	"github.com/intob/jfmt"
)

func main() {
	edges := []netip.AddrPort{
		netip.MustParseAddrPort("54.170.214.154:1618"),
		netip.MustParseAddrPort("3.249.184.30:1618"),
		netip.MustParseAddrPort("18.200.244.108:1618"),
	}
	edge := flag.Bool("e", false, "Start as edge-node, you'll be alone to begin.")
	lap := flag.String("l", "[::]:0", "Listen address:port")
	bap := flag.String("b", "", "Bootstrap address:port")
	dcap := flag.Uint("dc", 100000, "Dat map capacity")
	difficulty := flag.Int("d", 2, "For set command. Number of leading zeros.")
	hashonly := flag.Bool("h", false, "For set command. Output only dat hash.")
	timeout := flag.Duration("t", 10*time.Second, "For get command. Timeout.")
	stat := flag.Bool("stat", false, "For get command. Output stats.")
	all := flag.Bool("all", false, "For get command. Output all dats received.")
	verbose := flag.Bool("v", false, "Verbose logging. Use grep.")
	flush := flag.Uint64("flush", 1, "Flush log buffer frequency")
	flag.Parse()
	if *edge {
		edges = []netip.AddrPort{}
	}
	if *bap != "" {
		if strings.HasPrefix(*bap, ":") {
			*bap = "[::1]" + *bap
		}
		addr, err := netip.ParseAddrPort(*bap)
		if err != nil {
			exit(1, "failed to parse -p=%q: %v", *bap, err)
		}
		edges = []netip.AddrPort{addr}
	}
	laddr, err := net.ResolveUDPAddr("udp", *lap)
	if err != nil {
		exit(1, "failed to resolve UDP address: %v", err)
	}
	lch := make(chan string, 10)
	d, err := godave.NewDave(&godave.Cfg{
		Listen: laddr,
		Edges:  edges,
		DatCap: *dcap,
		Log:    lch})
	if err != nil {
		exit(1, "failed to make dave: %v", err)
	}
	if *hashonly {
		go func() {
			for range lch {
			}
		}()
	} else {
		go func(lch <-chan string) {
			var dlf *os.File
			if *verbose {
				dlf = os.Stdout
			} else {
				dlf, err = os.Open(os.DevNull)
				if err != nil {
					panic(err)
				}
			}
			defer dlf.Close()
			dlw := bufio.NewWriter(dlf)
			var n uint64
			for l := range lch {
				n++
				dlw.Write([]byte(l))
				if n%*flush == 0 {
					dlw.Flush()
				}
			}
		}(lch)
	}
	var action string
	if flag.NArg() > 0 {
		action = flag.Arg(0)
	}
	switch strings.ToLower(action) {
	case "set":
		if flag.NArg() < 2 {
			exit(1, "missing argument: set <VAL>")
		}
		set(d, []byte(flag.Arg(1)), *difficulty, *hashonly)
		return
	case "setfile":
		if flag.NArg() < 2 {
			exit(1, "missing argument: setfile <FILENAME>")
		}
		data, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			exit(2, "error reading file: %v", err)
		}
		set(d, data, *difficulty, *hashonly)
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
		var pass chan *dave.M
		if *all {
			pass = make(chan *dave.M, 10)
			go func() {
				for m := range pass {
					fmt.Printf("%s\n", m.V)
				}
			}()
		}
		var found bool
		for dat := range d.Get(work, *timeout, pass) {
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

func set(d *godave.Dave, val []byte, difficulty int, hashonly bool) {
	done := make(chan struct{})
	if hashonly {
		go func() {
			<-done
		}()
	} else {
		go func() {
			ti := time.NewTicker(time.Second)
			t := time.Now()
			for {
				select {
				case <-done:
					fmt.Print("\n")
					return
				case <-ti.C:
					fmt.Printf("\rworking for %s\033[0K", jfmt.FmtDuration(time.Since(t)))
				}
			}
		}()
	}
	dat := &godave.Dat{V: val, Ti: time.Now()}
	type sol struct{ work, salt []byte }
	solch := make(chan sol)
	ncpu := max(runtime.NumCPU()-2, 1)
	if !hashonly {
		fmt.Printf("running on %d cores\n", ncpu)
	}
	for n := 0; n < ncpu; n++ {
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
	time.Sleep(godave.EPOCH * godave.FANOUT)
	if hashonly {
		fmt.Printf("%x\n", dat.W)
	} else {
		fmt.Printf("\nWork: %x\nMass: %x\n", dat.W, godave.Mass(dat.W, dat.Ti))
	}
}

func exit(code int, msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(code)
}
