package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cometbft/cometbft/v2/p2p"
	"github.com/voluzi/cosmoseed/pkg/cosmoseed"
)

const (
	defaultConfigDir = ".cosmoseed"
	configFileName   = "config.yaml"
)

var (
	home, chainID, seeds, logLevel, externalAddress, podName string
	showVersion, showNodeID, configReadOnly                  bool
)

func init() {
	userHome, _ := os.UserHomeDir()
	defaultHome := filepath.Join(userHome, defaultConfigDir)

	flag.StringVar(&home,
		"home",
		defaultHome,
		"path to home",
	)
	flag.StringVar(&chainID,
		"chain-id",
		"",
		"chain ID to use",
	)
	flag.StringVar(&seeds,
		"seeds",
		"",
		"seeds to use",
	)
	flag.StringVar(&logLevel,
		"log-level",
		"",
		"logging level",
	)
	flag.StringVar(&externalAddress,
		"external-address",
		"",
		"external address to use in format '<host>:<port>'. "+
			"When pod-name is set, this can be a list separated by comma and index will be extracted "+
			"from pod name to chose the correct address (useful on kubernetes StatefulSets)",
	)
	flag.StringVar(&podName,
		"pod-name",
		"",
		"name of the pod when running on kubernetes. When set, node-key-file will be set to pod "+
			"name and index will be extracted from it to pick the right address from"+
			"external-address list (comma separated) (useful on kubernetes StatefulSets)",
	)

	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&showNodeID, "show-node-id", false, "print node ID and exit")
	flag.BoolVar(&configReadOnly, "config-read-only", false, "read-only mode for config file")
}

func loadEffectiveConfig(path string, lookup func(string) (string, bool), flags map[string]string) (*cosmoseed.Config, error) {
	cfg, err := cosmoseed.ReadConfigFromFile(path)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg, err = cosmoseed.DefaultConfig()
		if err != nil {
			return nil, err
		}
	}
	for _, item := range []struct {
		env, flag string
		field     *string
	}{
		{"CHAIN_ID", "chain-id", &cfg.ChainID},
		{"SEEDS", "seeds", &cfg.Seeds},
		{"LOG_LEVEL", "log-level", &cfg.LogLevel},
		{"EXTERNAL_ADDRESS", "external-address", &cfg.ExternalAddress},
	} {
		if value, ok := lookup(item.env); ok {
			*item.field = value
		}
		if value, ok := flags[item.flag]; ok {
			*item.field = value
		}
	}
	pod, _ := lookup("POD_NAME")
	if value, ok := flags["pod-name"]; ok {
		pod = value
	}
	if pod != "" {
		idx, err := extractIndexFromPodName(pod)
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(pod, `/\\`) {
			return nil, fmt.Errorf("invalid pod name %q", pod)
		}
		cfg.NodeKeyFile = pod
		if cfg.ExternalAddress != "" {
			if strings.Contains(cfg.ExternalAddress, ",") {
				addresses := strings.Split(cfg.ExternalAddress, ",")
				for i := range addresses {
					addresses[i] = strings.TrimSpace(addresses[i])
					if addresses[i] == "" {
						return nil, fmt.Errorf("external address list contains empty entry at index %d", i)
					}
				}
				if idx >= len(addresses) {
					return nil, fmt.Errorf("pod ordinal %d exceeds %d external addresses", idx, len(addresses))
				}
				cfg.ExternalAddress = addresses[idx]
			} else {
				cfg.ExternalAddress = strings.TrimSpace(cfg.ExternalAddress)
				if cfg.ExternalAddress == "" {
					return nil, fmt.Errorf("external address is empty")
				}
			}
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func resolveHome(defaultHome string, lookup func(string) (string, bool), flags map[string]string) string {
	home := defaultHome
	if value, ok := lookup("HOME_DIR"); ok {
		home = value
	}
	if value, ok := flags["home"]; ok {
		home = value
	}
	return home
}

func loadNodeID(home, configPath string, lookup func(string) (string, bool), flags map[string]string) (string, error) {
	cfg, err := cosmoseed.ReadConfigFromFile(configPath)
	if err != nil {
		return "", err
	}
	if cfg == nil {
		cfg, err = cosmoseed.DefaultConfig()
		if err != nil {
			return "", err
		}
	}
	pod, _ := lookup("POD_NAME")
	if value, ok := flags["pod-name"]; ok {
		pod = value
	}
	if pod != "" {
		if _, err := extractIndexFromPodName(pod); err != nil {
			return "", err
		}
		if strings.ContainsAny(pod, `/\`) {
			return "", fmt.Errorf("invalid pod name %q", pod)
		}
		cfg.NodeKeyFile = pod
	}
	keyPath := filepath.Join(home, cfg.NodeKeyFile)
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return "", err
	}
	key, err := p2p.LoadOrGenNodeKey(keyPath)
	if err != nil {
		return "", err
	}
	return key.ID(), nil
}
