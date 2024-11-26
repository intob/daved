package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	_ "embed"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/intob/daved/api"
	"github.com/intob/daved/cfg"
	"github.com/intob/godave"
	"github.com/intob/godave/logger"
	"github.com/intob/godave/pow"
	"github.com/intob/godave/store"
)

//go:embed commit
var commit string

type cmdOptions struct {
	DataKeyFilename string
	Difficulty      uint8
	Ntest           int
	Timeout         time.Duration
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

	// Execute command or wait for kill sig
	if flag.NArg() > 0 { // Command mode
		switch flag.Arg(0) {
		case "version":
			fmt.Printf("commit %s\n", commit)
		case "keygen":
			filename := cfg.DEFAULT_KEY_FILENAME
			if flag.NArg() < 2 {
				fmt.Printf("no filename provided, using default: %s\n", filename)
			} else {
				filename = flag.Arg(1)
			}
			_, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				exit(1, "failed to generate key: %s", err)
			}
			// TODO: encrypt key with passphrase
			os.WriteFile(filename, priv, 0600) // W/R by owner only
		case "put":
			d, _, err := initNode(nodeCfg)
			if err != nil {
				exit(1, "failed to init node: %s", err)
			}
			dataPrivateKey, err := cfg.ReadKeyFile(opt.DataKeyFilename)
			if err != nil {
				fmt.Printf("failed to read key file: %s\n", err)
				return
			}
			if flag.NArg() < 3 {
				exit(1, "missing arguments: put <KEY> <VAL>")
			}
			put(d, []byte(flag.Arg(1)), []byte(flag.Arg(2)), dataPrivateKey, opt)
			return
			/*case "get":
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
			*/
		}
	} else { // Node mode, wait for kill sig
		d, logs, err := initNode(nodeCfg)
		if err != nil {
			exit(1, "failed to init node: %s", err)
		}
		svc := api.NewService(&api.Cfg{
			ListenAddr: "127.0.0.1:8080",
			Logs:       logs,
			Dave:       d,
		})
		err = svc.Start()
		if err != nil {
			exit(1, "failed to start http server: %s", err)
		}
		<-getCtx().Done()
		<-d.Kill()
		fmt.Println("shutdown gracefully")
	}
}

func initNode(nodeCfg *cfg.NodeCfg) (*godave.Dave, chan<- string, error) {
	logs := make(chan string, 1)
	if flag.NArg() == 0 || nodeCfg.LogLevel == logger.DEBUG {
		go func() {
			if nodeCfg.FlushLogBuffer {
				for l := range logs {
					fmt.Println(l)
				}
			} else {
				dlw := bufio.NewWriter(os.Stdout)
				for l := range logs {
					fmt.Fprintln(dlw, l)
				}
			}
		}()
	}
	key, err := cfg.ReadKeyFile(nodeCfg.KeyFilename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load key file: %s", err)
	}
	socket, err := net.ListenUDP("udp", nodeCfg.UdpListenAddr)
	if err != nil {
		return nil, nil, err
	}
	d, err := godave.NewDave(&godave.Cfg{
		Socket:         socket,
		PrivateKey:     key,
		Edges:          nodeCfg.Edges,
		ShardCap:       nodeCfg.ShardCap,
		BackupFilename: nodeCfg.BackupFilename,
		Logger: logger.NewLogger(&logger.LoggerCfg{
			Level:  nodeCfg.LogLevel,
			Output: logs,
		}),
	})
	if err != nil {
		return nil, nil, err
	}
	return d, logs, nil
}

func parseFlags() (*cmdOptions, *cfg.NodeCfgUnparsed, string) {
	cfgFilename := flag.String("cfg", "", "Config filename")
	// CLI flags
	dataKeyFname := flag.String("data_key_filename", cfg.DEFAULT_KEY_FILENAME, "Data private key filename")
	difficulty := flag.Uint("d", godave.MIN_WORK, "For set command. Number of leading zero bits.")
	ntest := flag.Int("ntest", 1, "For set command. Repeat work & send n times. For testing.")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout for get command.")
	// Node flags
	nodeKeyFname := flag.String("node_key_filename", cfg.DEFAULT_KEY_FILENAME, "Node private key filename")
	udpLaddr := flag.String("udp_listen_addr", "", "Listen address:port")
	edges := flag.String("edges", "", "Comma-separated bootstrap address:port")
	backup := flag.String("backup_filename", "", "Backup file, set to enable.")
	shardCap := flag.Int("shard_cap", 0, "Shard capacity. There are 256 shards.")
	logLevel := flag.String("log_level", "", "Log level ERROR or DEBUG.")
	flush := flag.String("flush_log_buffer", "", "Flush log buffer after each write.")
	flag.Parse()
	opt := &cmdOptions{
		DataKeyFilename: *dataKeyFname,
		Difficulty:      uint8(*difficulty),
		Ntest:           *ntest,
		Timeout:         *timeout,
	}
	cfg := &cfg.NodeCfgUnparsed{
		KeyFilename:    *nodeKeyFname,
		UdpListenAddr:  *udpLaddr,
		Edges:          strings.Split(*edges, ","),
		BackupFilename: *backup,
		ShardCap:       *shardCap,
		LogLevel:       *logLevel,
		FlushLogBuffer: *flush,
	}
	return opt, cfg, *cfgFilename
}

func put(d *godave.Dave, key, val []byte, privKey ed25519.PrivateKey, opt *cmdOptions) {
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
					fmt.Printf("\rworking for %s\033[0K", time.Since(start))
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
		dat := &store.Dat{Key: keyInc, Val: val, Time: time.Now().Add(-100 * time.Millisecond)}
		dat.Work, dat.Salt = pow.DoWork(dat.Key, dat.Val, pow.Ttb(dat.Time), opt.Difficulty)
		dat.Sig = ed25519.Sign(privKey, dat.Work)
		dat.PubKey = privKey.Public().(ed25519.PublicKey)
		<-d.Put(*dat)
		if opt.Ntest > 1 {
			fmt.Printf("\rput %s\n", dat.Key)
		}
	}
	time.Sleep(50 * time.Millisecond) // Let sending finish (will improve this)
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
