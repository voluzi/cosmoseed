package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEffectiveConfigPrecedenceAndPodMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("chainID: file\nseeds: file-seed\nexternalAddress: file.example:26656\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CHAIN_ID": "env", "SEEDS": "", "EXTERNAL_ADDRESS": "first.example:26656,second.example:26656", "POD_NAME": "seed-1"}
	cfg, err := loadEffectiveConfig(path, func(k string) (string, bool) { v, ok := env[k]; return v, ok }, map[string]string{"chain-id": "flag"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChainID != "flag" || cfg.Seeds != "" || cfg.ExternalAddress != "second.example:26656" || cfg.NodeKeyFile != "seed-1" {
		t.Fatalf("unexpected precedence: %+v", cfg)
	}
}

func TestPodMappingFailsClosed(t *testing.T) {
	env := map[string]string{"CHAIN_ID": "test", "POD_NAME": "seed-2", "EXTERNAL_ADDRESS": "first.example:26656,second.example:26656"}
	_, err := loadEffectiveConfig(filepath.Join(t.TempDir(), "missing.yaml"), func(k string) (string, bool) { v, ok := env[k]; return v, ok }, nil)
	if err == nil {
		t.Fatal("out-of-range ordinal accepted")
	}
	if _, err := extractIndexFromPodName("seed--1"); err == nil {
		t.Fatal("negative ordinal accepted")
	}
}

func TestResolveHomeExplicitFlagWinsOverEnvironment(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == "HOME_DIR" {
			return "/env/home", true
		}
		return "", false
	}
	if got := resolveHome("/default/home", lookup, map[string]string{"home": "/flag/home"}); got != "/flag/home" {
		t.Fatalf("home = %s", got)
	}
	if got := resolveHome("/default/home", lookup, nil); got != "/env/home" {
		t.Fatalf("environment home = %s", got)
	}
}

func TestPodScalarExternalAddressSurvivesPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	env := map[string]string{"CHAIN_ID": "test", "POD_NAME": "seed-5", "EXTERNAL_ADDRESS": "single.example:26656"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	cfg, err := loadEffectiveConfig(path, lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	delete(env, "EXTERNAL_ADDRESS")
	cfg, err = loadEffectiveConfig(path, lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExternalAddress != "single.example:26656" {
		t.Fatalf("round trip address = %q", cfg.ExternalAddress)
	}
}

func TestPodMappingRejectsEmptyListEntries(t *testing.T) {
	for _, addresses := range []string{"a.example:26656,", ",b.example:26656", "a.example:26656, ,b.example:26656"} {
		env := map[string]string{"CHAIN_ID": "test", "POD_NAME": "seed-0", "EXTERNAL_ADDRESS": addresses}
		_, err := loadEffectiveConfig(filepath.Join(t.TempDir(), "config.yaml"), func(k string) (string, bool) { v, ok := env[k]; return v, ok }, nil)
		if err == nil {
			t.Errorf("accepted %q", addresses)
		}
	}
}

func TestInvalidLogLevelOverrideDoesNotRewriteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("chainID: test\nlogLevel: info\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	lookup := func(key string) (string, bool) {
		if key == "LOG_LEVEL" {
			return "unrecognized", true
		}
		return "", false
	}
	cfg, err := loadEffectiveConfig(path, lookup, nil)
	if err == nil {
		if err := cfg.Save(path); err != nil {
			t.Fatal(err)
		}
		t.Fatal("invalid LOG_LEVEL was accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("config changed after rejected override: %q", got)
	}
}

func TestShowNodeIDWorksWithoutOperationalConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	original := []byte("nodeKeyFile: offline-key.json\napiAddr: invalid\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	id, err := loadNodeID(home, path, func(string) (string, bool) { return "", false }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 40 {
		t.Fatalf("node ID = %q", id)
	}
	if _, err := os.Stat(filepath.Join(home, "offline-key.json")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("show-node-id rewrote config: %q", got)
	}
}

func TestInvalidSeedOverridesDoNotRewriteConfig(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef01234567"
	for _, tt := range []struct {
		name  string
		env   map[string]string
		flags map[string]string
		bad   string
	}{
		{name: "environment", env: map[string]string{"SEEDS": "bad@seed.invalid:26656"}, bad: "bad@seed.invalid:26656"},
		{name: "flag", env: map[string]string{"SEEDS": id + "@seed.example:26656"}, flags: map[string]string{"seeds": id + "@seed.invalid:not-a-port"}, bad: id + "@seed.invalid:not-a-port"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			original := []byte("chainID: test\nseeds: " + id + "@seed.example:26656\n")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			lookup := func(key string) (string, bool) { value, ok := tt.env[key]; return value, ok }
			cfg, err := loadEffectiveConfig(path, lookup, tt.flags)
			if err == nil {
				if err := cfg.Save(path); err != nil {
					t.Fatal(err)
				}
				t.Fatal("invalid seed override accepted")
			}
			if !strings.Contains(err.Error(), tt.bad) {
				t.Fatalf("error does not name bad seed: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(original) {
				t.Fatalf("config changed after rejected override: %q", got)
			}
			cfg, err = loadEffectiveConfig(path, func(string) (string, bool) { return "", false }, nil)
			if err != nil {
				t.Fatalf("override-free reload: %v", err)
			}
			if cfg.Seeds != id+"@seed.example:26656" {
				t.Fatalf("reloaded seeds = %q", cfg.Seeds)
			}
		})
	}
}
