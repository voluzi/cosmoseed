package cosmoseed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cometbft/cometbft/v2/p2p"
	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/cometbft/cometbft/v2/p2p/pex"
	"github.com/voluzi/cosmoseed/pkg/seedreactor"
)

type httpTestPeer struct {
	p2p.Peer
	addr *na.NetAddr
}

func (p httpTestPeer) IsOutbound() bool        { return true }
func (p httpTestPeer) ID() string              { return p.addr.ID }
func (p httpTestPeer) SocketAddr() *na.NetAddr { return p.addr }
func (p httpTestPeer) Send(p2p.Envelope) error { return nil }

func verifiedHTTPSeeder(t *testing.T) *Seeder {
	t.Helper()
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChainID = "test"
	book := pex.NewAddrBook(t.TempDir()+"/book.json", false)
	addr, err := na.NewFromString("0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.AddAddress(addr, addr); err != nil {
		t.Fatal(err)
	}
	r := seedreactor.NewReactor(book, nil, 10, 1, false, time.Hour, time.Minute, 5, time.Hour)
	r.AddPeer(httpTestPeer{addr: addr})
	s := &Seeder{cfg: cfg, book: book, pex: r}
	s.running.Store(true)
	return s
}

func TestHealthAndReadiness(t *testing.T) {
	s := &Seeder{}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/healthz", 200}, {"/readyz", 503}} {
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if r.Code != tc.status {
			t.Errorf("%s status = %d", tc.path, r.Code)
		}
	}
}

func TestPeersRejectsInvalidLimit(t *testing.T) {
	s := &Seeder{}
	r := httptest.NewRecorder()
	s.handlePeers(r, httptest.NewRequest(http.MethodGet, "/peers?limit=invalid", nil))
	if r.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", r.Code, r.Body.String())
	}
	if !strings.Contains(r.Body.String(), "limit") {
		t.Fatalf("body = %s", r.Body.String())
	}
}

func TestPeersRejectsEmptyAndDuplicateLimits(t *testing.T) {
	s := &Seeder{}
	for _, path := range []string{"/peers?limit=", "/peers?limit=1&limit=2"} {
		r := httptest.NewRecorder()
		s.handlePeers(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d", path, r.Code)
		}
	}
}

func TestPeerFormatsAndLimit(t *testing.T) {
	s := verifiedHTTPSeeder(t)
	for _, tc := range []struct{ path, want string }{
		{"/peers", "0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656"},
		{"/peers?format=toml", "persistent_peers = \"0123456789abcdef0123456789abcdef01234567@127.0.0.1:26656\"\n"},
	} {
		r := httptest.NewRecorder()
		s.handlePeers(r, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if r.Code != 200 || r.Body.String() != tc.want {
			t.Errorf("%s: %d %q", tc.path, r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	s.handlePeers(r, httptest.NewRequest(http.MethodGet, "/peers?format=json&limit=1", nil))
	var peers []map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &peers); err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0]["verified"] != true || peers[0]["id"] == "" || peers[0]["last_verified"] == "" {
		t.Fatalf("JSON peers: %s", r.Body.String())
	}
}

func TestReadinessRequiresVerifiedThreshold(t *testing.T) {
	s := verifiedHTTPSeeder(t)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	for _, tc := range []struct{ min, want int }{{1, 200}, {2, 503}, {0, 200}} {
		s.cfg.MinReadyPeers = tc.min
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if r.Code != tc.want {
			t.Errorf("min=%d got=%d", tc.min, r.Code)
		}
	}
	s.running.Store(false)
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if r.Code != 503 {
		t.Fatalf("stopped readiness = %d", r.Code)
	}
}

func TestPeersRejectsUnverifiedAndUnknownRoutes(t *testing.T) {
	s := verifiedHTTPSeeder(t)
	r := httptest.NewRecorder()
	s.handlePeers(r, httptest.NewRequest(http.MethodGet, "/peers?verified=false", nil))
	if r.Code != 400 {
		t.Fatalf("verified=false = %d", r.Code)
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	r = httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	if r.Code != 404 {
		t.Fatalf("unknown path = %d", r.Code)
	}
}

func TestEmptyJSONIsArray(t *testing.T) {
	s := &Seeder{}
	r := httptest.NewRecorder()
	s.handlePeers(r, httptest.NewRequest(http.MethodGet, "/peers?format=json", nil))
	if r.Body.String() != "[]\n" {
		t.Fatalf("empty JSON = %q", r.Body.String())
	}
}

func TestMetricsRegistryIsInstanceScoped(t *testing.T) {
	first := verifiedHTTPSeeder(t)
	second := verifiedHTTPSeeder(t)
	second.book.RemoveAddress(second.pex.GetPeerSelection()[0])
	for _, tc := range []struct {
		seeder *Seeder
		want   string
	}{{first, "cosmoseed_verified_peers 1"}, {second, "cosmoseed_verified_peers 0"}} {
		r := httptest.NewRecorder()
		tc.seeder.metricsHandler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if r.Code != 200 || !strings.Contains(r.Body.String(), tc.want) {
			t.Fatalf("metrics: %d %s", r.Code, r.Body.String())
		}
		if !strings.Contains(r.Body.String(), "cosmoseed_queue_capacity") || !strings.Contains(r.Body.String(), "cosmoseed_build_info") {
			t.Fatalf("missing metrics: %s", r.Body.String())
		}
	}
}
