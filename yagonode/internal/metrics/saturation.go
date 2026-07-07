package metrics

import "github.com/prometheus/client_golang/prometheus"

// SaturationMetrics counts admission rejections at the node's bounded intakes
// — the saturation events of the USE method: a rising rejection rate on a
// gate means that intake is at capacity, before latency or errors show it.
type SaturationMetrics struct {
	rejections *prometheus.CounterVec
}

// Intake gate labels: which bounded intake shed the request.
const (
	GateDHTTransfer  = "dht_transfer"
	GateRemoteSearch = "remote_search"
)

func NewSaturationMetrics(registry prometheus.Registerer) *SaturationMetrics {
	rejections := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "intake_rejections_total",
			Help: "Requests shed by a bounded intake gate, by gate.",
		},
		[]string{"gate"},
	)
	registry.MustRegister(rejections)

	return &SaturationMetrics{rejections: rejections}
}

// RejectionObserver returns the callback one gate invokes when it sheds a
// request; a nil receiver returns a no-op so wiring stays unconditional.
func (m *SaturationMetrics) RejectionObserver(gate string) func() {
	if m == nil {
		return func() {}
	}
	counter := m.rejections.WithLabelValues(gate)

	return func() { counter.Inc() }
}
