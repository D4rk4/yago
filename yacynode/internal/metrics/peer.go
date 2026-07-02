package metrics

import "github.com/prometheus/client_golang/prometheus"

type PeerMetrics struct {
	known          prometheus.Gauge
	active         prometheus.Gauge
	probeFailures  prometheus.Counter
	seedlistImport prometheus.Counter
}

func NewPeerMetrics(registry prometheus.Registerer) *PeerMetrics {
	known := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "yacy_peer_known_total",
		Help: "Current known peer roster entries.",
	})
	active := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "yacy_peer_active_total",
		Help: "Current reachable peer roster entries.",
	})
	probeFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_peer_probe_failures_total",
		Help: "Peer hello probes that failed during announcement cycles.",
	})
	seedlistImport := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_seedlist_imports_total",
		Help: "Configured seedlists fetched successfully.",
	})
	registry.MustRegister(known, active, probeFailures, seedlistImport)

	return &PeerMetrics{
		known:          known,
		active:         active,
		probeFailures:  probeFailures,
		seedlistImport: seedlistImport,
	}
}

func (m *PeerMetrics) ObservePeerRoster(known, active int) {
	m.known.Set(float64(known))
	m.active.Set(float64(active))
}

func (m *PeerMetrics) ObservePeerProbeFailure() {
	m.probeFailures.Inc()
}

func (m *PeerMetrics) ObserveSeedlistImport(int) {
	m.seedlistImport.Inc()
}
