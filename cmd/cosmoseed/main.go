package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	cosmoseed2 "github.com/voluzi/cosmoseed/pkg/cosmoseed"
)

func main() {
	flag.Parse()

	if showVersion {
		fmt.Printf("Version: %s\nCommit hash: %s\n", cosmoseed2.Version, cosmoseed2.CommitHash)
		os.Exit(0)
	}

	flags := map[string]string{}
	flag.Visit(func(f *flag.Flag) { flags[f.Name] = f.Value.String() })
	home = resolveHome(home, os.LookupEnv, flags)
	cfgPath := filepath.Join(home, configFileName)
	if showNodeID {
		id, err := loadNodeID(home, cfgPath, os.LookupEnv, flags)
		if err != nil {
			panic(err)
		}
		fmt.Println(id)
		return
	}
	cfg, err := loadEffectiveConfig(cfgPath, os.LookupEnv, flags)
	if err != nil {
		panic(err)
	}
	if !configReadOnly {
		if err := cfg.Save(cfgPath); err != nil {
			panic(err)
		}
	}

	seeder, err := cosmoseed2.NewSeeder(home, cfg)
	if err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = seeder.Run(ctx); err != nil {
		panic(err)
	}
}

func extractIndexFromPodName(podName string) (int, error) {
	parts := strings.Split(podName, "-")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return 0, fmt.Errorf("invalid pod name: %s", podName)
	}
	indexStr := parts[len(parts)-1]
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index from pod name %q: %w", podName, err)
	}
	return index, nil
}
