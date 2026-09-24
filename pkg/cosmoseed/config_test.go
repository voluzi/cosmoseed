package cosmoseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadConfigOverlaysDefaultsAndRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("chainID: test\nmaxOutboundPeers: 0\nallowNonRoutable: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxOutboundPeers != 0 || !cfg.AllowNonRoutable || cfg.PeerQueueSize != 1000 {
		t.Fatalf("overlay lost explicit zero or defaults: %+v", cfg)
	}
	if err := os.WriteFile(path, []byte("chainID: test\nunknownField: yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConfigFromFile(path); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestConfigSaveIsAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("unexpected temp files: %v", entries)
	}
}

func TestConfigValidationRejectsInvalidAddresses(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.ExternalAddress = "2001:db8::1:26656"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "externalAddress") {
		t.Fatalf("got %v", err)
	}
	cfg.ExternalAddress = "[2001:db8::1]:26656"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.ExternalAddress = ":26656"
	if err := cfg.Validate(); err == nil {
		t.Fatal("external address without host accepted")
	}
	cfg.ExternalAddress = "example.com:http"
	if err := cfg.Validate(); err == nil {
		t.Fatal("named service port accepted")
	}
}

func TestDurationConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("chainID: test\nverificationTTL: 15m\nrecheckInterval: 30s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VerificationTTL.String() != "15m0s" || cfg.RecheckInterval.String() != "30s" {
		t.Fatalf("durations = %v, %v", cfg.VerificationTTL, cfg.RecheckInterval)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConfigFromFile(path); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRejectsTrailingYAMLDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("chainID: first\n---\nchainID: second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConfigFromFile(path); err == nil {
		t.Fatal("trailing YAML document accepted")
	}
}

func TestConfigAcceptsZeroPeerLimitsAndDisabledMetrics(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.MaxInboundPeers = 0
	cfg.MaxOutboundPeers = 0
	cfg.MetricsAddr = ""
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
