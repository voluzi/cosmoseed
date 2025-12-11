package main

import (
	"flag"
	"os"
	"path"

	"github.com/voluzi/cosmoseed/internal/utils"
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
	defaultHome := path.Join(userHome, defaultConfigDir)

	flag.StringVar(&home,
		"home",
		utils.GetString("HOME_DIR", defaultHome),
		"path to home",
	)
	flag.StringVar(&chainID,
		"chain-id",
		utils.GetString("CHAIN_ID", ""),
		"chain ID to use",
	)
	flag.StringVar(&seeds,
		"seeds",
		utils.GetString("SEEDS", ""),
		"seeds to use",
	)
	flag.StringVar(&logLevel,
		"log-level",
		utils.GetString("LOG_LEVEL", "info"),
		"logging level",
	)
	flag.StringVar(&externalAddress,
		"external-address",
		utils.GetString("EXTERNAL_ADDRESS", ""),
		"external address to use in format '<host>:<port>'. "+
			"When pod-name is set, this can be a list separated by comma and index will be extracted "+
			"from pod name to chose the correct address (useful on kubernetes StatefulSets)",
	)
	flag.StringVar(&podName,
		"pod-name",
		utils.GetString("POD_NAME", ""),
		"name of the pod when running on kubernetes. When set, node-key-file will be set to pod "+
			"name and index will be extracted from it to pick the right address from"+
			"external-address list (comma separated) (useful on kubernetes StatefulSets)",
	)

	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&showNodeID, "show-node-id", false, "print node ID and exit")
	flag.BoolVar(&configReadOnly, "config-read-only", false, "read-only mode for config file")
}
