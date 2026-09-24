package cosmoseed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/voluzi/cosmoseed/pkg/seedreactor"
)

func (s *Seeder) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.seedAddress)
	mux.HandleFunc("/peers", s.handlePeers)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.isReady() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/status", s.handleStatus)
}

func (s *Seeder) seedAddress(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write([]byte(s.GetFullAddress()))
}

func (s *Seeder) handlePeers(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if values, present := r.URL.Query()["limit"]; present {
		if len(values) != 1 || values[0] == "" {
			http.Error(w, "limit must be between 1 and 100", http.StatusBadRequest)
			return
		}
		var err error
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > 100 {
			http.Error(w, "limit must be between 1 and 100", http.StatusBadRequest)
			return
		}
	}
	format := r.URL.Query().Get("format")
	if values, ok := r.URL.Query()["verified"]; ok {
		if len(values) != 1 || values[0] != "true" {
			http.Error(w, "only verified peers are available", http.StatusBadRequest)
			return
		}
	}
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" && format != "toml" {
		http.Error(w, "unsupported format", http.StatusBadRequest)
		return
	}
	var peers []seedreactor.VerifiedPeer
	if s.pex != nil {
		peers = s.pex.GetPeerDetails()
	}
	if len(peers) > limit {
		peers = peers[:limit]
	}
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		if peers == nil {
			peers = []seedreactor.VerifiedPeer{}
		}
		_ = json.NewEncoder(w).Encode(peers)
	case "toml":
		w.Header().Set("Content-Type", "application/toml")
		addresses := make([]string, 0, len(peers))
		for _, p := range peers {
			addresses = append(addresses, p.Address)
		}
		_, _ = fmt.Fprintf(w, "persistent_peers = %q\n", strings.Join(addresses, ","))
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		list := make([]string, 0, len(peers))
		for _, p := range peers {
			list = append(list, p.Address)
		}
		_, _ = w.Write([]byte(strings.Join(list, ",")))
	}
}

func (s *Seeder) handleStatus(w http.ResponseWriter, _ *http.Request) {
	verified, candidates := 0, 0
	if s.pex != nil {
		verified = s.pex.VerifiedCount()
	}
	if s.book != nil {
		candidates = s.book.Size()
	}
	chainID := ""
	if s.cfg != nil {
		chainID = s.cfg.ChainID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"version": Version, "chainID": chainID, "ready": s.isReady(), "verifiedPeers": verified, "candidatePeers": candidates})
}

func (s *Seeder) isReady() bool {
	if !s.running.Load() {
		return false
	}
	min := 1
	if s.cfg != nil {
		min = s.cfg.MinReadyPeers
	}
	return s.pex != nil && s.pex.VerifiedCount() >= min
}
