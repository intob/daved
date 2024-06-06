package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/intob/godave"
	"github.com/intob/jfmt"
)

//go:embed commit
var commit string

func main() {
	laddrstr := flag.String("l", "[::]:127", "Listen address:port")
	edge := flag.String("e", "", "Edge bootstrap address:port")
	backup := flag.String("b", "", "Backup file, set to enable.")
	difficulty := flag.Uint("d", godave.MINWORK, "For set command. Number of leading zero bits.")
	test := flag.Bool("t", false, "Test mode. Permits full use of port space per IP.")
	flush := flag.Bool("f", false, "Flush log buffer after each write.")
	epoch := flag.Duration("epoch", 20*time.Microsecond, "Base cycle period. Reduce to increase bandwidth usage.")
	scap := flag.Int("scap", 10000, "Dat shard capacity. Each shard corresponds to a difficulty level.")
	fcap := flag.Uint("fcap", 1000, "Cuckoo filter capacity.)")
	prune := flag.Int("prune", 20000, "Interval in epochs between refreshing dat & peer maps & writing backup.")
	rounds := flag.Int("rounds", 3, "For set command. Number of times to repeat sending dat.")
	ntest := flag.Int("ntest", 1, "For set command. Repeat work & send n times. For testing.")
	timeout := flag.Duration("timeout", 5*time.Second, "For get command. Timeout.")
	stat := flag.Bool("stat", false, "For get command. Output performance measurements.")
	flag.Parse()
	laddr, err := net.ResolveUDPAddr("udp", *laddrstr)
	if err != nil {
		exit(1, "failed to resolve UDP listen address: %v", err)
	}
	lch := make(chan []byte, 100)
	go func(lch <-chan []byte) {
		dlw := bufio.NewWriter(os.Stdout)
		for l := range lch {
			fmt.Fprint(dlw, string(l))
			if *flush {
				dlw.Flush()
			}
		}
	}(lch)
	edges := []netip.AddrPort{}
	if *edge != "" {
		edges, err = parseAddrPortMaybeHostname(*edge)
		if err != nil {
			exit(1, fmt.Sprintf("failed to parse addr: %s", err))
		}
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
	if flag.NArg() > 0 { // COMMAND MODE
		switch flag.Arg(0) {
		case "version":
			fmt.Printf("commit %s\n", commit)
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
		}
	} else { // NODE MODE
		<-getCtx().Done()
		<-d.Kill()
		fmt.Println("shutdown beautifully")
	}
}

func parseAddrPortMaybeHostname(edge string) ([]netip.AddrPort, error) {
	addrs := make([]netip.AddrPort, 0)
	portStart := strings.LastIndex(edge, ":")
	if portStart < 0 || portStart >= len(edge) {
		return nil, errors.New("missing port")
	}
	port := edge[portStart+1:]
	host := edge[:portStart]
	ip := net.ParseIP(host)
	if ip != nil { // host is an IP address
		addrPort, err := parseAddrPort(net.JoinHostPort(ip.String(), port))
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addrPort)
	} else { // host is a hostname, lookup IP addresses
		hostAddrs, err := net.LookupHost(host)
		if err != nil {
			return nil, err
		}
		for _, addr := range hostAddrs {
			addrPort, err := parseAddrPort(net.JoinHostPort(addr, port))
			if err != nil {
				return nil, err
			}
			addrs = append(addrs, addrPort)
		}
	}
	return addrs, nil
}

func parseAddrPort(addrport string) (netip.AddrPort, error) {
	if strings.HasPrefix(addrport, ":") { // infer local machine if no IP
		addrport = "[::1]" + addrport
	}
	parsed, err := netip.ParseAddrPort(addrport)
	if err != nil {
		return parsed, fmt.Errorf("failed to parse addr %q: %w", addrport, err)
	}
	return parsed, nil
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

func cancelOnKillSig(sigs chan os.Signal, cancel context.CancelFunc) {
	switch <-sigs {
	case syscall.SIGINT:
		fmt.Println("\nreceived SIGINT")
	case syscall.SIGTERM:
		fmt.Println("\nreceived SIGTERM")
	}
	cancel()
}

func getCtx() context.Context {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	go cancelOnKillSig(sigs, cancel)
	return ctx
}
