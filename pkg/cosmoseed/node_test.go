package cosmoseed

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func distinctAvailableAddresses(t *testing.T, count int) []string {
	t.Helper()
	listeners := make([]net.Listener, 0, count)
	addresses := make([]string, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		addresses = append(addresses, listener.Addr().String())
	}
	for _, listener := range listeners {
		_ = listener.Close()
	}
	return addresses
}

func TestRunReturnsMetricsBindErrorAndReleasesAPI(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.AllowNonRoutable = true
	addresses := distinctAvailableAddresses(t, 2)
	cfg.ListenAddr = "tcp://" + addresses[0]
	cfg.ApiAddr = addresses[1]
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()
	cfg.MetricsAddr = occupied.Addr().String()
	s, err := NewSeeder(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "listen metrics") {
		t.Fatalf("Run error = %v", err)
	}
	api, err := net.Listen("tcp", cfg.ApiAddr)
	if err != nil {
		t.Fatalf("API listener leaked: %v", err)
	}
	_ = api.Close()
}

func TestRunStopsOnContextAndStopIsIdempotent(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.AllowNonRoutable = true
	addresses := distinctAvailableAddresses(t, 3)
	cfg.ListenAddr = "tcp://" + addresses[0]
	cfg.ApiAddr = addresses[1]
	cfg.MetricsAddr = addresses[2]
	s, err := NewSeeder(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	client := http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + cfg.ApiAddr + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	metrics, err := client.Get("http://" + cfg.MetricsAddr + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d", metrics.StatusCode)
	}
	_ = metrics.Body.Close()
	mainMetrics, err := client.Get("http://" + cfg.ApiAddr + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if mainMetrics.StatusCode != http.StatusNotFound {
		t.Fatalf("main API exposed metrics: %d", mainMetrics.StatusCode)
	}
	_ = mainMetrics.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("port %s remained bound: %v", address, err)
		}
		_ = listener.Close()
	}
}

func TestPrestartStopPreservesAddrbook(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	home := t.TempDir()
	bookPath := filepath.Join(home, cfg.AddrBookFile)
	if err := os.MkdirAll(filepath.Dir(bookPath), 0700); err != nil {
		t.Fatal(err)
	}
	contents := []byte(`{"key":"preserve"}`)
	if err := os.WriteFile(bookPath, contents, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSeeder(home, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if err := s.Stop(); err != nil {
				t.Errorf("Stop: %v", err)
			}
		})
	}
	wg.Wait()
	got, err := os.ReadFile(bookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(contents) {
		t.Fatalf("pre-start Stop rewrote addrbook: %s", got)
	}
}

func TestRunWithDisabledMetrics(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.AllowNonRoutable = true
	addresses := distinctAvailableAddresses(t, 2)
	cfg.ListenAddr = "tcp://" + addresses[0]
	cfg.ApiAddr = addresses[1]
	cfg.MetricsAddr = ""
	s, err := NewSeeder(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.running.Load() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !s.running.Load() {
		t.Fatal("node did not start")
	}
	if s.metricsServer != nil {
		t.Fatal("metrics server created with empty address")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReactorStartupFailureReleasesListeners(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.AllowNonRoutable = true
	cfg.Seeds = "not-a-seed-address"
	addresses := distinctAvailableAddresses(t, 3)
	cfg.ListenAddr = "tcp://" + addresses[0]
	cfg.ApiAddr = addresses[1]
	cfg.MetricsAddr = addresses[2]
	s, err := NewSeeder(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Run(t.Context()); err == nil {
		t.Fatal("invalid seed accepted")
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("port %s remained bound: %v", address, err)
		}
		_ = listener.Close()
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateRunDoesNotStopActiveInstance(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	cfg.AllowNonRoutable = true
	addresses := distinctAvailableAddresses(t, 3)
	cfg.ListenAddr = "tcp://" + addresses[0]
	cfg.ApiAddr = addresses[1]
	cfg.MetricsAddr = addresses[2]
	s, err := NewSeeder(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	client := http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	healthy := func() bool {
		response, err := client.Get("http://" + cfg.ApiAddr + "/healthz")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == 200
	}
	for time.Now().Before(deadline) {
		if s.running.Load() && healthy() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !s.running.Load() || !healthy() {
		t.Fatal("first Run did not start")
	}
	if err := s.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("duplicate Run = %v", err)
	}
	if !healthy() || !s.running.Load() {
		t.Fatal("first Run stopped after duplicate Run")
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err == nil {
			_ = listener.Close()
			t.Fatalf("active port %s was released", address)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first Run did not stop")
	}
}
