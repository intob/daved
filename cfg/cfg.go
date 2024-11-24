package cfg

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/intob/godave/logger"
	"gopkg.in/yaml.v3"
)

type NodeCfg struct {
	UdpListenAddr  *net.UDPAddr
	Edges          []netip.AddrPort
	BackupFilename string
	ShardCap       int
	LogLevel       logger.LogLevel
	FlushLogBuffer bool
}

type NodeCfgUnparsed struct {
	UdpListenAddr  string   `yaml:"udp_listen_addr"`
	Edges          []string `yaml:"edges"`
	BackupFilename string   `yaml:"backup_filename"`
	ShardCap       int      `yaml:"shard_cap"`
	LogLevel       string   `yaml:"log_level"`
	FlushLogBuffer string   `yaml:"flush_log_buffer"`
}

var defaultCfgUnparsed = NodeCfgUnparsed{
	UdpListenAddr: "[::]:127",
	ShardCap:      10_000,
	LogLevel:      "ERROR",
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

func MergeConfigs(a, b NodeCfgUnparsed) *NodeCfgUnparsed {
	if b.UdpListenAddr != "" {
		a.UdpListenAddr = b.UdpListenAddr
	}
	if len(b.Edges) > 0 {
		a.Edges = append(a.Edges, b.Edges...)
	}
	if b.BackupFilename != "" {
		a.BackupFilename = b.BackupFilename
	}
	if b.ShardCap != 0 {
		a.ShardCap = b.ShardCap
	}
	if b.LogLevel != "" {
		a.LogLevel = b.LogLevel
	}
	if b.FlushLogBuffer != "" {
		a.FlushLogBuffer = b.FlushLogBuffer
	}
	return &a
}

func ParseNodeCfg(unparsed *NodeCfgUnparsed) (*NodeCfg, error) {
	withDefaults := MergeConfigs(defaultCfgUnparsed, *unparsed)
	var err error
	cfg := &NodeCfg{}
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
	cfg.BackupFilename = withDefaults.BackupFilename
	cfg.ShardCap = withDefaults.ShardCap
	if strings.ToUpper(withDefaults.LogLevel) == "DEBUG" {
		cfg.LogLevel = logger.DEBUG
	} else {
		cfg.LogLevel = logger.ERROR
	}
	if withDefaults.FlushLogBuffer != "" {
		cfg.FlushLogBuffer = true
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
