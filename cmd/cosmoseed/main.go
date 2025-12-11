package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	cosmoseed2 "github.com/voluzi/cosmoseed/pkg/cosmoseed"
)

func main() {
	flag.Parse()

	if showVersion {
		fmt.Printf("Version: %s\nCommit hash: %s\n", cosmoseed2.Version, cosmoseed2.CommitHash)
		os.Exit(0)
	}

	cfgPath := path.Join(home, configFileName)

	cfg, err := cosmoseed2.ReadConfigFromFile(cfgPath)
	if err != nil {
		panic(err)
	}

	if cfg == nil {
		cfg, err = cosmoseed2.DefaultConfig()
		if err != nil {
			panic(err)
		}
	}

	if !configReadOnly {
		if err = cfg.Save(cfgPath); err != nil {
			panic(err)
		}
	}

	if chainID != "" {
		cfg.ChainID = chainID
	}

	if seeds != "" {
		cfg.Seeds = seeds
	}

	if logLevel != "" {
		cfg.LogLevel = logLevel
	}

	if externalAddress != "" {
		cfg.ExternalAddress = externalAddress
	}

	if podName != "" {
		cfg.NodeKeyFile = podName
		if externalAddress != "" {
			idx, _ := extractIndexFromPodName(podName)
			parts := strings.Split(externalAddress, ",")
			if len(parts) > idx {
				cfg.ExternalAddress = parts[idx]
			}
		}
	}

	seeder, err := cosmoseed2.NewSeeder(home, cfg)
	if err != nil {
		panic(err)
	}

	if showNodeID {
		fmt.Println(seeder.GetNodeID())
		os.Exit(0)
	}

	if err = seeder.Start(); err != nil {
		panic(err)
	}
}

func extractIndexFromPodName(podName string) (int, error) {
	parts := strings.Split(podName, "-")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid pod name: %s", podName)
	}
	indexStr := parts[len(parts)-1]
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index from pod name %q: %w", podName, err)
	}
	return index, nil
}
