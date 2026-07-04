package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPeerMetricsReportsRosterLevels(t *testing.T) {
	observer := NewPeerMetrics(prometheus.NewRegistry())

	observer.ObservePeerRoster(7, 3)

	if got := testutil.ToFloat64(observer.known); got != 7 {
		t.Fatalf("known peers = %v, want 7", got)
	}
	if got := testutil.ToFloat64(observer.active); got != 3 {
		t.Fatalf("active peers = %v, want 3", got)
	}
}

func TestPeerMetricsCountsEvents(t *testing.T) {
	observer := NewPeerMetrics(prometheus.NewRegistry())

	observer.ObservePeerProbeFailure()
	observer.ObservePeerProbeFailure()
	observer.ObserveSeedlistImport(12)

	if got := testutil.ToFloat64(observer.probeFailures); got != 2 {
		t.Fatalf("probe failures = %v, want 2", got)
	}
	if got := testutil.ToFloat64(observer.seedlistImport); got != 1 {
		t.Fatalf("seedlist imports = %v, want 1", got)
	}
}
