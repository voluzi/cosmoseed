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

func TestEmptyAndCommentOnlyConfigOverlayDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, content := range []string{"", "# comment only\n  # another comment\n"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := ReadConfigFromFile(path)
		if err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		if cfg == nil || cfg.LogLevel != "info" || cfg.PeerQueueSize != 1000 {
			t.Fatalf("%q defaults = %+v", content, cfg)
		}
	}
}

func TestConfigValidationOrderAndListenerOverlap(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.ApiAddr = "invalid"
	cfg.MetricsAddr = "invalid"
	for range 20 {
		if err := cfg.Validate(); err == nil || !strings.HasPrefix(err.Error(), "apiAddr:") {
			t.Fatalf("validation order = %v", err)
		}
	}
	cfg.ApiAddr = "0.0.0.0:9090"
	cfg.MetricsAddr = "127.0.0.1:9090"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("wildcard overlap = %v", err)
	}
	cfg.ApiAddr = "[::]:9090"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("IPv6 wildcard overlap = %v", err)
	}
	cfg.ApiAddr = "127.0.0.1:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("distinct ports rejected: %v", err)
	}
	for _, tc := range []struct{ api, metrics string }{
		{"127.0.0.1:9090", "[::1]:9090"},
		{"0.0.0.0:9090", "[::1]:9090"},
	} {
		cfg.ApiAddr, cfg.MetricsAddr = tc.api, tc.metrics
		if err := cfg.Validate(); err != nil {
			t.Errorf("separate IP families %s + %s rejected: %v", tc.api, tc.metrics, err)
		}
	}
	for _, tc := range []struct{ api, metrics string }{
		{"0.0.0.0:9090", "127.0.0.1:9090"},
		{"[::]:9090", "[::1]:9090"},
	} {
		cfg.ApiAddr, cfg.MetricsAddr = tc.api, tc.metrics
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Errorf("same-family overlap %s + %s accepted: %v", tc.api, tc.metrics, err)
		}
	}
}

func TestConfigValidatesSeedSyntaxWithoutResolvingHostnames(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name    string
		seeds   string
		wantErr bool
		bad     string
	}{
		{name: "empty"},
		{name: "empty list entries are ignored", seeds: " ,  , "},
		{name: "IPv4", seeds: id + "@127.0.0.1:26656"},
		{name: "IPv6", seeds: id + "@[2001:db8::1]:26656"},
		{name: "hostnames and trimming", seeds: " " + id + "@seed.example:26656,  " + id + "@other.example:26657 "},
		{name: "optional TCP prefix", seeds: "tcp://" + id + "@seed.example:26656"},
		{name: "optional UDP prefix", seeds: "udp://" + id + "@seed.example:26656"},
		{name: "unresolved hostname", seeds: id + "@seed.invalid:26656"},
		{name: "missing ID separator", seeds: id + ":26656", wantErr: true, bad: id + ":26656"},
		{name: "missing ID", seeds: "@seed.example:26656", wantErr: true, bad: "@seed.example:26656"},
		{name: "malformed ID", seeds: "bad@seed.example:26656", wantErr: true, bad: "bad@seed.example:26656"},
		{name: "extra ID separator", seeds: id + "@@seed.example:26656", wantErr: true, bad: id + "@@seed.example:26656"},
		{name: "missing host", seeds: id + "@:26656", wantErr: true, bad: id + "@:26656"},
		{name: "unbracketed IPv6", seeds: id + "@2001:db8::1:26656", wantErr: true, bad: id + "@2001:db8::1:26656"},
		{name: "named port", seeds: id + "@seed.example:http", wantErr: true, bad: id + "@seed.example:http"},
		{name: "negative port", seeds: id + "@seed.example:-1", wantErr: true, bad: id + "@seed.example:-1"},
		{name: "out of range port", seeds: id + "@seed.example:65536", wantErr: true, bad: id + "@seed.example:65536"},
		{name: "bad port on unresolved hostname", seeds: id + "@seed.invalid:not-a-port", wantErr: true, bad: id + "@seed.invalid:not-a-port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatal(err)
			}
			cfg.ChainID = "test"
			cfg.Seeds = tt.seeds
			err = cfg.Validate()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "seeds") || !strings.Contains(err.Error(), tt.bad) {
					t.Fatalf("Validate(%q) = %v", tt.seeds, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate(%q) = %v", tt.seeds, err)
			}
		})
	}
}
