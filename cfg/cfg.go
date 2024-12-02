package cfg

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/intob/godave/logger"
	"gopkg.in/yaml.v3"
)

const DEFAULT_KEY_FILENAME = "key.dave"

var defaultCfgUnparsed = NodeCfgUnparsed{
	KeyFilename:   DEFAULT_KEY_FILENAME,
	UdpListenAddr: "[::]:127",
	ShardCapacity: 1024 * 1024 * 1024,   // 1GB
	TTL:           365 * 24 * time.Hour, // 1 year
	LogLevel:      "ERROR",
}

type NodeCfg struct {
	KeyFilename    string
	UdpListenAddr  *net.UDPAddr
	Edges          []netip.AddrPort
	BackupFilename string
	ShardCapacity  int64
	TTL            time.Duration
	LogLevel       logger.LogLevel
	LogUnbuffered  bool
}

type NodeCfgUnparsed struct {
	KeyFilename    string        `yaml:"key_filename"`
	UdpListenAddr  string        `yaml:"udp_listen_addr"`
	Edges          []string      `yaml:"edges"`
	BackupFilename string        `yaml:"backup_filename"`
	ShardCapacity  int64         `yaml:"shard_capacity"`
	TTL            time.Duration `yaml:"ttl"`
	LogLevel       string        `yaml:"log_level"`
	LogUnbuffered  string        `yaml:"log_unbuffered"`
}

func ReadNodeCfgFile(filename string) (*NodeCfgUnparsed, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %s", err)
	}
	dec := yaml.NewDecoder(file)
	cfg := &NodeCfgUnparsed{}
	err = dec.Decode(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to decode yaml: %s", err)
	}
	return cfg, nil
}

// Merges src with dst.
// If a field in src is omitted, the value in dst is left unchanged.
func MergeConfigs(dst, src NodeCfgUnparsed) *NodeCfgUnparsed {
	if src.KeyFilename != "" {
		dst.KeyFilename = src.KeyFilename
	}
	if src.UdpListenAddr != "" {
		dst.UdpListenAddr = src.UdpListenAddr
	}
	if len(src.Edges) > 0 {
		dst.Edges = append(dst.Edges, src.Edges...)
	}
	if src.BackupFilename != "" {
		dst.BackupFilename = src.BackupFilename
	}
	if src.ShardCapacity != 0 {
		dst.ShardCapacity = src.ShardCapacity
	}
	if src.TTL != 0 {
		dst.TTL = src.TTL
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
	}
	if src.LogUnbuffered != "" {
		dst.LogUnbuffered = src.LogUnbuffered
	}
	return &dst
}

func ParseNodeCfg(unparsed *NodeCfgUnparsed) (*NodeCfg, error) {
	withDefaults := MergeConfigs(defaultCfgUnparsed, *unparsed)
	cfg := &NodeCfg{
		KeyFilename:    withDefaults.KeyFilename,
		BackupFilename: withDefaults.BackupFilename,
		ShardCapacity:  withDefaults.ShardCapacity,
		TTL:            withDefaults.TTL,
	}
	var err error
	cfg.UdpListenAddr, err = net.ResolveUDPAddr("udp", withDefaults.UdpListenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve UDP listen address: %s", err)
	}
	cfg.Edges = make([]netip.AddrPort, 0)
	for _, e := range withDefaults.Edges {
		if e == "" {
			continue
		}
		addrs, err := parseAddrPortOrHostname(e)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve address or hostname: %s", err)
		}
		cfg.Edges = append(cfg.Edges, addrs...)
	}

	if strings.ToUpper(withDefaults.LogLevel) == "DEBUG" {
		cfg.LogLevel = logger.DEBUG
	} else {
		cfg.LogLevel = logger.ERROR
	}
	if withDefaults.LogUnbuffered != "" {
		cfg.LogUnbuffered = true
	}
	return cfg, nil
}

func parseAddrPortOrHostname(edge string) ([]netip.AddrPort, error) {
	addrs := make([]netip.AddrPort, 0)
	portStart := strings.LastIndex(edge, ":")
	if portStart < 0 || portStart == len(edge) {
		return nil, errors.New("missing port")
	}
	port := edge[portStart+1:]
	host := edge[:portStart]
	if host == "" { // default to local machine
		host = "::ffff:127.0.0.1"
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

func ReadKeyFile(filename string) (ed25519.PrivateKey, error) {
	key, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid key file, expected %d bytes, got %d", ed25519.PrivateKeySize, len(key))
	}
	return key, nil
}
