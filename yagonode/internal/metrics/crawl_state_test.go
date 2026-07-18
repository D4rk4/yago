package metrics

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

type crawlStateCapacityFixture struct {
	used int64
	err  error
}

func (f crawlStateCapacityFixture) UsedBytes(context.Context) (int64, error) {
	return f.used, f.err
}

func TestCrawlStateMetricsExposeCapacityAndHighWater(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewCrawlStateMetrics(
		registry,
		crawlStateCapacityFixture{used: 32},
		func() (int64, error) { return 48, nil },
	)
	if metrics.used == nil || metrics.highWater == nil {
		t.Fatalf("crawl state metrics = %#v", metrics)
	}
	want := map[string]float64{
		"crawl_broker_state_used_bytes": 32,
		"crawl_broker_state_file_bytes": 48,
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		name := family.GetName()
		expected, found := want[name]
		if found && family.GetMetric()[0].GetGauge().GetValue() != expected {
			t.Fatalf(
				"%s = %v, want %v",
				name,
				family.GetMetric()[0].GetGauge().GetValue(),
				expected,
			)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing metrics = %v", want)
	}
}

func TestCrawlStateMetricsReportMeasurementFailures(t *testing.T) {
	registry := prometheus.NewRegistry()
	NewCrawlStateMetrics(
		registry,
		crawlStateCapacityFixture{err: errors.New("used failed")},
		func() (int64, error) { return 0, errors.New("stat failed") },
	)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if value := family.GetMetric()[0].GetGauge().GetValue(); !math.IsNaN(value) {
			t.Fatalf("%s = %v, want NaN", family.GetName(), value)
		}
	}
}
