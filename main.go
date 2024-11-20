package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/intob/godave"
	"github.com/intob/jfmt"
)

//go:embed commit
var commit string

func main() {
	laddrStr := flag.String("l", "[::]:127", "Listen address:port")
	edge := flag.String("e", "", "Edge bootstrap address:port")
	backup := flag.String("b", "", "Backup file, set to enable.")
	keyFname := flag.String("key", "key.dave", "Private key filename")
	difficulty := flag.Uint("d", godave.MINWORK, "For set command. Number of leading zero bits.")
	flush := flag.Bool("f", false, "Flush log buffer after each write.")
	shardCap := flag.Int("shardcap", 10000, "Shard capacity. There are 256 shards.")
	npeer := flag.Int("npeer", 3, "For set & get commands. Number of peers to wait for before sending messages.")
	ntest := flag.Int("ntest", 1, "For set command. Repeat work & send n times. For testing.")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout for get command.")
	stat := flag.Bool("stat", false, "For get command. Output performance measurements.")
	logLvl := flag.Int("loglvl", int(godave.LOGLVL_ERROR), "Log level: 0=ERROR,1=DEBUG")
	flag.Parse()
	laddr, err := net.ResolveUDPAddr("udp", *laddrStr)
	if err != nil {
		exit(1, "failed to resolve UDP listen address: %v", err)
	}
	lch := make(chan string, 1)
	if flag.NArg() == 0 || *logLvl == int(godave.LOGLVL_DEBUG) { // NODE MODE OR DEBUG
		go func() {
			if *flush {
				for l := range lch {
					fmt.Println(l)
				}
			} else {
				dlw := bufio.NewWriter(os.Stdout)
				for l := range lch {
					fmt.Fprintln(dlw, l)
				}
			}
		}()
	}
	addrs := []netip.AddrPort{}
	if *edge != "" {
		addrs, err = parseAddrPortMaybeHostname(*edge)
		if err != nil {
			exit(1, "failed to parse addr: %s", err)
		}
	}
	var privKey ed25519.PrivateKey
	privKeyRaw, err := os.ReadFile(*keyFname)
	if err != nil {
		fmt.Printf("failed to read key file: %s\n", err)
	} else {
		privKey = ed25519.PrivateKey(privKeyRaw)
	}
	d, err := godave.NewDave(&godave.Cfg{
		UdpListenAddr: laddr,
		Edges:         addrs,
		ShardCap:      *shardCap,
		Log:           lch,
		BackupFname:   *backup,
		LogLvl:        godave.LogLvl(*logLvl),
	})
	if err != nil {
		exit(1, "failed to make dave: %v", err)
	}
	if flag.NArg() > 0 { // COMMAND MODE
		switch flag.Arg(0) {
		case "version":
			fmt.Printf("commit %s\n", commit)
		case "keygen":
			if flag.NArg() < 2 {
				exit(1, "missing argument: keygen <FILENAME>")
			}
			_, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				exit(1, "failed to generate key: %s", err)
			}
			// TODO: encrypt key with passphrase
			os.WriteFile(flag.Arg(1), priv, 0600) // W/R by owner only
		case "set":
			if flag.NArg() < 3 {
				exit(1, "missing arguments: set <KEY> <VAL>")
			}
			set(d, []byte(flag.Arg(1)), []byte(flag.Arg(2)), privKey, uint8(*difficulty), *npeer, *ntest)
			return
		case "setf":
			if flag.NArg() < 3 {
				exit(1, "missing arguments: setf <KEY> <FILENAME>")
			}
			data, err := os.ReadFile(flag.Arg(2))
			if err != nil {
				exit(2, "error reading file: %v", err)
			}
			set(d, []byte(flag.Arg(1)), data, privKey, uint8(*difficulty), *npeer, *ntest)
		case "get":
			if flag.NArg() < 2 {
				exit(1, "correct usage is get <WORK>")
			}
			tstart := time.Now()
			var found bool
			for dat := range d.Get([]byte(flag.Arg(1)), int32(*npeer), *timeout) {
				found = true
				fmt.Println(string(dat.Val))
				if *stat {
					fmt.Printf("t: %s\n", time.Since(tstart))
				}
			}
			if !found {
				exit(1, "dat not found")
			}
		}
	} else { // NODE MODE
		<-getCtx().Done()
		<-d.Kill()
		fmt.Println("shutdown gracefully")
	}
}

func parseAddrPortMaybeHostname(edge string) ([]netip.AddrPort, error) {
	addrs := make([]netip.AddrPort, 0)
	portStart := strings.LastIndex(edge, ":")
	if portStart < 0 || portStart == len(edge) {
		return nil, errors.New("missing port")
	}
	port := edge[portStart+1:]
	host := edge[:portStart]
	if host == "" { // default to local machine
		host = "127.0.0.1"
	}
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
		if len(addrs) > 1 {
			for _, addr := range addrs {
				if addr.Addr().Is4() || addr.Addr().Is4In6() { // prioritise IPv4
					return []netip.AddrPort{addr}, nil
				}
			}
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

func set(d *godave.Dave, key, val []byte, privKey ed25519.PrivateKey, difficulty uint8, npeer, ntest int) {
	done := make(chan struct{}, 1)
	if ntest == 1 { // don't log time if we're sending loads to test
		start := time.Now()
		go func() {
			tick := time.NewTicker(time.Second)
			for {
				select {
				case <-done:
					fmt.Printf("\rdone\033[0K")
					return
				case <-tick.C:
					fmt.Printf("\rworking for %s\033[0K", jfmt.FmtDuration(time.Since(start)))
				}
			}
		}()
	}
	keyInc := key
	for i := 0; i < ntest; i++ {
		if i > 0 {
			keyInc = []byte(fmt.Sprintf("%s_%d", key, i))
		}
		// 100ms margin, incase clocks are not well synchronised
		dat := &godave.Dat{Key: keyInc, Val: val, Time: time.Now().Add(-100 * time.Millisecond)}
		dat.Work, dat.Salt = godave.Work(dat.Key, dat.Val, godave.Ttb(dat.Time), difficulty)
		dat.Sig = ed25519.Sign(privKey, dat.Work)
		dat.PubKey = privKey.Public().(ed25519.PublicKey)
		<-d.Set(dat, int32(npeer))
		if ntest > 1 {
			fmt.Printf("\r\nput %s\n", dat.Key)
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
