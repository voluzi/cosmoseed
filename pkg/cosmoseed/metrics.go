package cosmoseed

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (s *Seeder) metricsHandler() http.Handler {
	registry := prometheus.NewRegistry()
	gauge := func(name, help string, value func() float64) {
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, value))
	}
	counter := func(name, help string, value func() float64) {
		registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: help}, value))
	}
	gauge("cosmoseed_candidate_peers", "Addresses in the candidate book.", func() float64 { return float64(s.book.Size()) })
	gauge("cosmoseed_verified_peers", "Fresh authenticated outbound peers.", func() float64 { return float64(s.pex.VerifiedCount()) })
	gauge("cosmoseed_ready", "Whether the seed is ready to serve peers.", func() float64 {
		if s.isReady() {
			return 1
		}
		return 0
	})
	gauge("cosmoseed_queue_depth", "Verification jobs waiting in the queue.", func() float64 { return float64(s.pex.Stats().QueueDepth) })
	gauge("cosmoseed_queue_capacity", "Maximum queued verification jobs.", func() float64 { return float64(s.pex.Stats().QueueCapacity) })
	gauge("cosmoseed_dialing_peers", "Outbound connections currently dialing.", func() float64 {
		if s.sw == nil {
			return 0
		}
		_, _, dialing := s.sw.NumPeers()
		return float64(dialing)
	})
	for _, direction := range []string{"inbound", "outbound"} {
		direction := direction
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cosmoseed_connected_peers", Help: "Connected peers by direction.", ConstLabels: prometheus.Labels{"direction": direction}}, func() float64 {
			if s.sw == nil {
				return 0
			}
			outbound, inbound, _ := s.sw.NumPeers()
			if direction == "inbound" {
				return float64(inbound)
			}
			return float64(outbound)
		}))
	}
	counter("cosmoseed_dial_attempts_total", "Outbound verification dial attempts.", func() float64 { return float64(s.pex.Stats().Attempts) })
	counter("cosmoseed_dial_failures_total", "Failed outbound verification dials.", func() float64 { return float64(s.pex.Stats().DialFailures) })
	counter("cosmoseed_authenticated_total", "Authenticated outbound peers.", func() float64 { return float64(s.pex.Stats().Authenticated) })
	counter("cosmoseed_received_addresses_total", "Addresses received in PEX responses.", func() float64 { return float64(s.pex.Stats().Received) })
	counter("cosmoseed_queued_total", "Verification jobs queued.", func() float64 { return float64(s.pex.Stats().Queued) })
	counter("cosmoseed_dropped_total", "Verification jobs dropped because the queue was full.", func() float64 { return float64(s.pex.Stats().Dropped) })
	counter("cosmoseed_evictions_total", "Old verification records evicted from memory.", func() float64 { return float64(s.pex.Stats().Evictions) })
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "cosmoseed_build_info", Help: "Build identity.", ConstLabels: prometheus.Labels{"version": Version, "commit": CommitHash}}, func() float64 { return 1 }))
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return mux
}
