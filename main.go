package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/intob/daved/cfg"
	"github.com/intob/godave"
	"github.com/intob/jfmt"
)

//go:embed commit
var commit string

type cmdOptions struct {
	KeyFilename string
	Difficulty  uint8
	NPeer       int
	Ntest       int
	Timeout     time.Duration
}

func main() {
	// Parse & merge configuration
	opt, cfgFlags, cfgFilename := parseFlags()
	unparsedCfg := cfgFlags
	if cfgFilename != "" {
		cfgFile, err := cfg.ReadNodeCfgFile(cfgFilename)
		if err != nil {
			exit(1, "failed to read config file: %s", err)
		}
		unparsedCfg = cfg.MergeConfigs(*cfgFile, *cfgFlags) // flags take precedence
	}
	nodeCfg, err := cfg.ParseNodeCfg(unparsedCfg)
	if err != nil {
		exit(1, "failed to parse config: %s", err)
	}

	// Logging
	lch := make(chan string, 1)
	if flag.NArg() == 0 || nodeCfg.LogLevel == godave.LOGLEVEL_DEBUG {
		go func() {
			if nodeCfg.FlushLogBuffer {
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

	// Init node
	d, err := godave.NewDave(&godave.Cfg{
		UdpListenAddr: nodeCfg.UdpListenAddr,
		Edges:         nodeCfg.Edges,
		ShardCap:      nodeCfg.ShardCap,
		BackupFname:   nodeCfg.BackupFilename,
		LogLevel:      nodeCfg.LogLevel,
		Log:           lch,
	})
	if err != nil {
		exit(1, "failed to make dave: %v", err)
	}

	// Execute command or wait for kill sig
	if flag.NArg() > 0 { // Command mode
		var privKey ed25519.PrivateKey
		var pubKey ed25519.PublicKey
		privKeyRaw, err := os.ReadFile(opt.KeyFilename)
		if err != nil {
			fmt.Printf("failed to read key file: %s\n", err)
		} else {
			privKey = ed25519.PrivateKey(privKeyRaw)
			pubKey = privKey.Public().(ed25519.PublicKey)
		}
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
			set(d, []byte(flag.Arg(1)), []byte(flag.Arg(2)), privKey, opt)
			return
		case "setf":
			if flag.NArg() < 3 {
				exit(1, "missing arguments: setf <KEY> <FILENAME>")
			}
			data, err := os.ReadFile(flag.Arg(2))
			if err != nil {
				exit(2, "error reading file: %v", err)
			}
			set(d, []byte(flag.Arg(1)), data, privKey, opt)
		case "get":
			if flag.NArg() < 2 {
				exit(1, "correct usage is get <KEY>")
			}
			tstart := time.Now()
			var found bool
			for dat := range d.Get(pubKey, []byte(flag.Arg(1)), opt.Timeout) {
				found = true
				fmt.Println(string(dat.Val))
				fmt.Printf("t: %s\n", time.Since(tstart))
			}
			if !found {
				exit(1, "dat not found")
			}
		}
	} else { // Node mode, wait for kill sig
		<-getCtx().Done()
		<-d.Kill()
		fmt.Println("shutdown gracefully")
	}
}

func parseFlags() (*cmdOptions, *cfg.NodeCfgUnparsed, string) {
	cfgFilename := flag.String("cfg", "", "Config filename")
	// CLI flags
	keyFname := flag.String("key", "key.dave", "Private key filename")
	difficulty := flag.Uint("d", godave.MINWORK, "For set command. Number of leading zero bits.")
	npeer := flag.Int("npeer", 3, "For set command. Number of peers to wait for before sending.")
	ntest := flag.Int("ntest", 1, "For set command. Repeat work & send n times. For testing.")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout for get command.")
	// Node flags
	udpLaddr := flag.String("udp_listen_addr", "", "Listen address:port")
	edges := flag.String("edges", "", "Comma-separated bootstrap address:port")
	backup := flag.String("backup_filename", "", "Backup file, set to enable.")
	shardCap := flag.Int("shard_cap", 0, "Shard capacity. There are 256 shards.")
	logLevel := flag.String("log_level", "", "Log level ERROR or DEBUG.")
	flush := flag.String("flush_log_buffer", "", "Flush log buffer after each write.")
	flag.Parse()
	opt := &cmdOptions{
		KeyFilename: *keyFname,
		Difficulty:  uint8(*difficulty),
		NPeer:       *npeer,
		Ntest:       *ntest,
		Timeout:     *timeout,
	}
	cfg := &cfg.NodeCfgUnparsed{
		UdpListenAddr:  *udpLaddr,
		Edges:          strings.Split(*edges, ","),
		BackupFilename: *backup,
		ShardCap:       *shardCap,
		LogLevel:       *logLevel,
		FlushLogBuffer: *flush,
	}
	return opt, cfg, *cfgFilename
}

func set(d *godave.Dave, key, val []byte, privKey ed25519.PrivateKey, opt *cmdOptions) {
	done := make(chan struct{}, 1)
	if opt.Ntest == 1 { // don't log time if we're sending loads to test
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
	for i := 0; i < opt.Ntest; i++ {
		if i > 0 {
			keyInc = []byte(fmt.Sprintf("%s_%d", key, i))
		}
		// 100ms margin, incase clocks are not well synchronised
		dat := &godave.Dat{Key: keyInc, Val: val, Time: time.Now().Add(-100 * time.Millisecond)}
		dat.Work, dat.Salt = godave.DoWork(dat.Key, dat.Val, godave.Ttb(dat.Time), opt.Difficulty)
		dat.Sig = ed25519.Sign(privKey, dat.Work)
		dat.PubKey = privKey.Public().(ed25519.PublicKey)
		waitForPeers(d, int32(opt.NPeer))
		<-d.Set(dat)
		if opt.Ntest > 1 {
			fmt.Printf("\r\nput %s\n", dat.Key)
		}
	}
	done <- struct{}{}
}

func waitForPeers(d *godave.Dave, npeer int32) {
	for {
		if d.PeerCount() >= npeer {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
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
