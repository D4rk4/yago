package yagonode

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

func TestAttachCrawlStateMetricsReportsDedicatedDatabaseSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	state, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open crawl state: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	registry := prometheus.NewRegistry()
	attachCrawlStateMetrics(
		&crawlRuntime{state: state, ownsState: true, statePath: path},
		registry,
	)

	value := gatheredGaugeValue(t, registry, "crawl_broker_state_file_bytes")
	if value <= 0 {
		t.Fatalf("crawl broker state file bytes = %v, want positive size", value)
	}
}

func TestAttachCrawlStateMetricsReportsMissingDatabaseFile(t *testing.T) {
	state := openTestVault(t)
	registry := prometheus.NewRegistry()
	attachCrawlStateMetrics(
		&crawlRuntime{
			state: state, ownsState: true,
			statePath: filepath.Join(t.TempDir(), "missing.db"),
		},
		registry,
	)

	value := gatheredGaugeValue(t, registry, "crawl_broker_state_file_bytes")
	if !math.IsNaN(value) {
		t.Fatalf("missing crawl broker state file bytes = %v, want NaN", value)
	}
}

func gatheredGaugeValue(
	t *testing.T,
	registry *prometheus.Registry,
	name string,
) float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("metric %q not registered", name)
	return 0
}
