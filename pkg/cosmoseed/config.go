package cosmoseed

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cometbft/cometbft/v2/libs/log"
	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/creasty/defaults"
	"gopkg.in/yaml.v3"
)

type Config struct {
	NodeKeyFile  string `yaml:"nodeKeyFile" default:"node_key.json"`
	AddrBookFile string `yaml:"addrBookFile" default:"addrbook.json"`

	ListenAddr string `yaml:"listenAddr" default:"tcp://0.0.0.0:26656"`
	LogLevel   string `yaml:"logLevel" default:"info"`

	MaxInboundPeers         int  `yaml:"maxInboundPeers" default:"2000"`
	MaxOutboundPeers        int  `yaml:"maxOutboundPeers" default:"20"`
	MaxPacketMsgPayloadSize int  `yaml:"maxPacketMsgPayloadSize" default:"1024"`
	AllowNonRoutable        bool `yaml:"allowNonRoutable"`
	PeerQueueSize           int  `yaml:"peerQueueSize" default:"1000"`
	DialWorkers             int  `yaml:"dialWorkers" default:"20"`

	ChainID         string `yaml:"chainID"`
	Seeds           string `yaml:"seeds"`
	ExternalAddress string `yaml:"externalAddress,omitempty"`

	ApiAddr         string        `yaml:"apiAddr" default:"0.0.0.0:8080"`
	MetricsAddr     string        `yaml:"metricsAddr" default:"127.0.0.1:9090"`
	VerificationTTL time.Duration `yaml:"verificationTTL" default:"30m"`
	RecheckInterval time.Duration `yaml:"recheckInterval" default:"1m"`
	MinReadyPeers   int           `yaml:"minReadyPeers" default:"1"`
	MaxDialFailures int           `yaml:"maxDialFailures" default:"5"`
	BanDuration     time.Duration `yaml:"banDuration" default:"1h"`
}

func (cfg *Config) Save(path string) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if err = ensurePath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func DefaultConfig() (*Config, error) {
	cfg := &Config{}
	return cfg, defaults.Set(cfg)
}

func ReadConfigFromFile(path string) (*Config, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("error reading config file: %v", err)
	}
	cfg, err := DefaultConfig()
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(f)))
	decoder.KnownFields(true)
	err = decoder.Decode(cfg)
	if errors.Is(err, io.EOF) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("decode config file: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("decode trailing config: %w", err)
		}
		return nil, errors.New("config file contains multiple YAML documents")
	}
	return cfg, nil
}

func (cfg *Config) Validate() error {
	if cfg.ChainID == "" {
		return errors.New("chainID is required")
	}
	if _, err := log.AllowLevel(cfg.LogLevel); err != nil {
		return fmt.Errorf("logLevel: %w", err)
	}
	for _, seed := range splitAndTrimEmpty(cfg.Seeds, ",", " ") {
		if err := validateSeedAddress(seed); err != nil {
			return fmt.Errorf("seeds: invalid entry %q: %w", seed, err)
		}
	}
	for _, item := range []struct{ name, address string }{{"apiAddr", cfg.ApiAddr}, {"metricsAddr", cfg.MetricsAddr}, {"externalAddress", cfg.ExternalAddress}} {
		name, address := item.name, item.address
		if (name == "externalAddress" || name == "metricsAddr") && address == "" {
			continue
		}
		if err := validateHostPort(address, name == "externalAddress"); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if !strings.HasPrefix(cfg.ListenAddr, "tcp://") {
		return errors.New("listenAddr must use tcp://")
	}
	if err := validateHostPort(strings.TrimPrefix(cfg.ListenAddr, "tcp://"), false); err != nil {
		return fmt.Errorf("listenAddr: %w", err)
	}
	if cfg.MaxInboundPeers < 0 || cfg.MaxOutboundPeers < 0 || cfg.MaxPacketMsgPayloadSize <= 0 || cfg.PeerQueueSize <= 0 || cfg.DialWorkers <= 0 {
		return errors.New("peer limits must be nonnegative; packet size, queue size, and dial workers must be positive")
	}
	if cfg.VerificationTTL <= 0 || cfg.RecheckInterval <= 0 || cfg.BanDuration <= 0 {
		return errors.New("verificationTTL, recheckInterval, and banDuration must be positive")
	}
	if cfg.MaxDialFailures <= 0 || cfg.MinReadyPeers < 0 {
		return errors.New("maxDialFailures must be positive and minReadyPeers nonnegative")
	}
	if cfg.MetricsAddr != "" && listenerAddressesOverlap(cfg.ApiAddr, cfg.MetricsAddr) {
		return errors.New("apiAddr and metricsAddr overlap")
	}
	return nil
}

func validateSeedAddress(seed string) error {
	if scheme, address, ok := strings.Cut(seed, "://"); ok {
		if !strings.EqualFold(scheme, "tcp") && !strings.EqualFold(scheme, "udp") {
			return errors.New("unsupported protocol")
		}
		seed = address
	}
	parts := strings.Split(seed, "@")
	if len(parts) != 2 {
		return errors.New("expected exactly one ID@address separator")
	}
	if err := na.ValidateID(parts[0]); err != nil {
		return fmt.Errorf("invalid peer ID: %w", err)
	}
	host, port, err := net.SplitHostPort(parts[1])
	if err != nil {
		return err
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("empty host")
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid TCP port: %w", err)
	}
	if parsedPort == 0 {
		return errors.New("invalid TCP port: must be between 1 and 65535")
	}
	return nil
}

func listenerAddressesOverlap(first, second string) bool {
	firstHost, firstPort, _ := net.SplitHostPort(first)
	secondHost, secondPort, _ := net.SplitHostPort(second)
	if firstPort != secondPort {
		return false
	}
	firstHost, secondHost = strings.ToLower(firstHost), strings.ToLower(secondHost)
	if firstHost == secondHost {
		return true
	}
	if firstHost == "" || secondHost == "" {
		return true
	}
	firstIP, secondIP := net.ParseIP(firstHost), net.ParseIP(secondHost)
	if firstIP != nil && secondIP != nil {
		if firstIP.IsUnspecified() && firstIP.To4() == nil {
			return true
		}
		if secondIP.IsUnspecified() && secondIP.To4() == nil {
			return true
		}
		if (firstIP.To4() == nil) != (secondIP.To4() == nil) {
			return false
		}
		return firstIP.IsUnspecified() || secondIP.IsUnspecified() || firstIP.Equal(secondIP)
	}
	if firstHost == "localhost" || secondHost == "localhost" {
		other := firstHost
		if firstHost == "localhost" {
			other = secondHost
		}
		otherIP := net.ParseIP(other)
		return other == "localhost" || otherIP != nil && (otherIP.IsLoopback() || otherIP.IsUnspecified())
	}
	return firstIP != nil && firstIP.IsUnspecified() || secondIP != nil && secondIP.IsUnspecified()
}

func validateHostPort(address string, requireHost bool) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host == "" && requireHost {
		return errors.New("empty host")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("invalid TCP port")
	}
	return nil
}
