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
	"github.com/intob/jfmt"
)

func main() {
	bootstraps := []netip.AddrPort{
		netip.MustParseAddrPort("54.170.214.154:1618"),
		netip.MustParseAddrPort("3.249.184.30:1618"),
		netip.MustParseAddrPort("18.200.244.108:1618"),
	}
	seed := flag.Bool("s", false, "start as seed, no bootstraps")
	lap := flag.String("l", "[::]:0", "listen address:port")
	bap := flag.String("b", "", "bootstrap address:port")
	dcap := flag.Uint("dc", 500000, "Dat map capacity")
	fcap := flag.Uint("fc", 1000000, "Cuckoo filter capacity")
	verbose := flag.Bool("v", false, "Verbose logging. Use grep.")
	difficulty := flag.Int("d", 2, "For set command. Number of leading zeros.")
	hashonly := flag.Bool("h", false, "For set command. Output only dat hash.")
	flag.Parse()
	if *seed {
		bootstraps = []netip.AddrPort{}
	}
	if *bap != "" {
		if strings.HasPrefix(*bap, ":") {
			*bap = "[::1]" + *bap
		}
		addr, err := netip.ParseAddrPort(*bap)
		if err != nil {
			exit(1, "failed to parse -p=%q: %v", *bap, err)
		}
		bootstraps = []netip.AddrPort{addr}
	}
	laddr, err := net.ResolveUDPAddr("udp", *lap)
	if err != nil {
		exit(1, "failed to resolve UDP address: %v", err)
	}
	lch := make(chan string, 4)
	d, err := godave.NewDave(&godave.Cfg{
		Listen:     laddr,
		Bootstraps: bootstraps,
		DatCap:     *dcap,
		FilterCap:  *fcap,
		Log:        lch})
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
			for l := range lch {
				dlw.Write([]byte(l))
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
		time.Sleep(time.Second)
		dat := <-d.Get(work, time.Second)
		if dat == nil {
			exit(1, "dat not found")
		}
		fmt.Println(string(dat.V))
		return
	}
	if *verbose {
		for range d.Recv { // dave logs enough
		}
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
		go func() {
			work, salt := godave.Work(dat.V, godave.Ttb(dat.Ti), difficulty)
			solch <- sol{work, salt}
		}()
	}
	s := <-solch
	dat.W = s.work
	dat.S = s.salt
	done <- struct{}{}
	<-d.Set(*dat)
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
