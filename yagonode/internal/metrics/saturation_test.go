package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSaturationMetricsCountRejectionsPerGate(t *testing.T) {
	registry := prometheus.NewRegistry()
	saturation := NewSaturationMetrics(registry)

	reject := saturation.RejectionObserver(GateDHTTransfer)
	reject()
	reject()
	saturation.RejectionObserver(GateRemoteSearch)()

	expected := strings.NewReader(
		`# HELP intake_rejections_total Requests shed by a bounded intake gate, by gate.
# TYPE intake_rejections_total counter
intake_rejections_total{gate="dht_transfer"} 2
intake_rejections_total{gate="remote_search"} 1
`,
	)
	if err := testutil.GatherAndCompare(registry, expected, "intake_rejections_total"); err != nil {
		t.Fatalf("gathered metrics: %v", err)
	}

	var nilMetrics *SaturationMetrics
	nilMetrics.RejectionObserver("anything")()
}
