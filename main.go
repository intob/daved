package main

import (
	"context"
	"crypto/ed25519"
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
	"github.com/intob/godave/types"
)

//go:embed commit
var commit string

type cmdOptions struct {
	DataKeyFilename string
	Difficulty      uint8
	Ntest           int
	Timeout         time.Duration
	PeerCount       int
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
			_, priv, err := ed25519.GenerateKey(nil)
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
			keyFilename := opt.DataKeyFilename
			if keyFilename == "" { // fallback to node key file
				keyFilename = nodeCfg.KeyFilename
			}
			dataPrivateKey, err := cfg.ReadKeyFile(keyFilename)
			if err != nil {
				fmt.Printf("failed to read key file: %s\n", err)
				return
			}
			if flag.NArg() < 3 {
				exit(1, "missing arguments: put <KEY> <VAL>")
			}
			put(d, flag.Arg(1), []byte(flag.Arg(2)), dataPrivateKey, opt)
		case "get":
			if flag.NArg() < 2 {
				exit(1, "correct usage is get <KEY>")
			}
			d, _, err := initNode(nodeCfg)
			if err != nil {
				exit(1, "failed to init node: %s", err)
			}
			keyFilename := opt.DataKeyFilename
			if keyFilename == "" { // fallback to node key file
				keyFilename = nodeCfg.KeyFilename
			}
			dataPrivateKey, err := cfg.ReadKeyFile(keyFilename)
			if err != nil {
				fmt.Printf("failed to read key file: %s\n", err)
				return
			}
			d.WaitForActivePeers(context.Background(), opt.PeerCount)
			dat, err := d.Get(&types.Get{
				PublicKey: dataPrivateKey.Public().(ed25519.PublicKey),
				DatKey:    flag.Arg(1)})
			if err != nil {
				exit(1, err.Error())
			}
			fmt.Printf("%s=%s\n", dat.Key, string(dat.Val))
			d.Kill()
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
		d.Kill()
		fmt.Println("shutdown gracefully")
	}
}

func initNode(nodeCfg *cfg.NodeCfg) (*godave.Dave, chan<- string, error) {
	var logs chan<- string
	if flag.NArg() == 0 || nodeCfg.LogLevel == logger.DEBUG {
		// If running as node (not CLI), or log level is debug, print logs
		logs = logger.StdOut(!nodeCfg.LogUnbuffered)
	} else {
		logs = logger.DevNull()
	}
	key, err := cfg.ReadKeyFile(nodeCfg.KeyFilename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load key file: %s", err)
	}
	socket, err := net.ListenUDP("udp", nodeCfg.UdpListenAddr)
	if err != nil {
		return nil, nil, err
	}
	logger, err := logger.NewDaveLogger(&logger.DaveLoggerCfg{
		Level:  nodeCfg.LogLevel,
		Output: logs,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create logger: %w", err)
	}
	d, err := godave.NewDave(&godave.DaveCfg{
		Socket:         socket,
		PrivateKey:     key,
		Edges:          nodeCfg.Edges,
		ShardCapacity:  nodeCfg.ShardCapacity,
		BackupFilename: nodeCfg.BackupFilename,
		Logger:         logger,
	})
	if err != nil {
		return nil, nil, err
	}
	return d, logs, nil
}

func parseFlags() (*cmdOptions, *cfg.NodeCfgUnparsed, string) {
	cfgFilename := flag.String("cfg", "", "Config filename")
	// CLI flags
	dataKeyFname := flag.String("data_key_filename", "", "Data private key filename")
	difficulty := flag.Uint("d", godave.MIN_WORK, "For set command. Number of leading zero bits.")
	ntest := flag.Int("ntest", 1, "For put command. Repeat work & send n times. For testing.")
	timeout := flag.Duration("timeout", 10*time.Second, "Timeout for get command.")
	npeer := flag.Int("npeer", 1, "Number of peers to wait for.")
	// Node flags
	nodeKeyFname := flag.String("node_key_filename", "", "Node private key filename")
	udpLaddr := flag.String("udp_listen_addr", "", "Listen address:port")
	edges := flag.String("edges", "", "Comma-separated bootstrap address:port")
	backup := flag.String("backup_filename", "", "Backup file, set to enable.")
	shardCap := flag.Int64("shard_capacity", 0, "Shard capacity. There are 256 shards.")
	logLevel := flag.String("log_level", "", "Log level ERROR or DEBUG.")
	logUnbuffered := flag.String("log_unbuffered", "", "Flush log buffer after each write.")
	flag.Parse()
	opt := &cmdOptions{
		DataKeyFilename: *dataKeyFname,
		Difficulty:      uint8(*difficulty),
		Ntest:           *ntest,
		Timeout:         *timeout,
		PeerCount:       *npeer,
	}
	cfg := &cfg.NodeCfgUnparsed{
		KeyFilename:    *nodeKeyFname,
		UdpListenAddr:  *udpLaddr,
		Edges:          strings.Split(*edges, ","),
		BackupFilename: *backup,
		ShardCapacity:  *shardCap,
		LogLevel:       *logLevel,
		LogUnbuffered:  *logUnbuffered,
	}
	return opt, cfg, *cfgFilename
}

func put(d *godave.Dave, key string, val []byte, privKey ed25519.PrivateKey, opt *cmdOptions) {
	fmt.Printf("waiting for %d peers...\n", opt.PeerCount)
	d.WaitForActivePeers(context.Background(), opt.PeerCount)
	keyInc := key
	for i := 0; i < opt.Ntest; i++ {
		if i > 0 {
			keyInc = fmt.Sprintf("%s_%d", key, i)
		}
		// 100ms margin, incase clocks are not well synchronised
		dat := &types.Dat{Key: keyInc, Val: val, Time: time.Now().Add(-100 * time.Millisecond)}
		fmt.Println("computing proof...")
		dat.Work, dat.Salt = pow.DoWork(dat.Key, dat.Val, dat.Time, opt.Difficulty)
		dat.Sig = types.Signature(ed25519.Sign(privKey, dat.Work[:]))
		dat.PubKey = privKey.Public().(ed25519.PublicKey)
		err := d.Put(*dat)
		if err != nil {
			exit(1, err.Error())
		}
		fmt.Printf("put %s\n", dat.Key)
	}
	time.Sleep(50 * time.Millisecond) // Let sending finish (will improve this)
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
