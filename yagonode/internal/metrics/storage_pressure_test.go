package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

type fixedStoragePressure struct {
	snapshot yagocrawlcontract.StoragePressureSnapshot
}

func (s fixedStoragePressure) Snapshot() yagocrawlcontract.StoragePressureSnapshot {
	return s.snapshot
}

func TestStoragePressureMetricsExposePolicyStateAndCounters(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := metrics.NewStoragePressureMetrics(registry, fixedStoragePressure{
		snapshot: yagocrawlcontract.StoragePressureSnapshot{
			Policy: yagocrawlcontract.StoragePressurePolicy{
				ReservedFreeBytes: 40, RecoveryHysteresisBytes: 10,
			},
			AvailableBytes: 35, Pressured: true,
			RejectedGrowthTotal: 7, MeasurementFailuresTotal: 3,
		},
	})
	if collector == nil {
		t.Fatal("storage pressure metrics not constructed")
	}
	wants := map[string]float64{
		"storage_filesystem_available_bytes":          35,
		"storage_reserved_free_bytes":                 40,
		"storage_pressure_hysteresis_bytes":           10,
		"storage_pressure":                            1,
		"storage_pressure_measurement_available":      0,
		"storage_growth_rejections_total":             7,
		"storage_pressure_measurement_failures_total": 3,
	}
	for name, want := range wants {
		if got := testutil.ToFloat64(metricCollector(t, registry, name)); got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}

func metricCollector(
	t *testing.T,
	registry *prometheus.Registry,
	name string,
) prometheus.Collector {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return prometheus.NewGaugeFunc(
				prometheus.GaugeOpts{Name: "test_" + name},
				func() float64 {
					metric := family.GetMetric()[0]
					if metric.Gauge != nil {
						return metric.GetGauge().GetValue()
					}

					return metric.GetCounter().GetValue()
				},
			)
		}
	}
	t.Fatalf("metric %s not found", name)

	return nil
}

func TestStoragePressureMetricBooleanFalseAndTrue(t *testing.T) {
	if got := metricsTestBoolMetric(false); got != 0 {
		t.Fatalf("false metric = %v", got)
	}
	registry := prometheus.NewRegistry()
	metrics.NewStoragePressureMetrics(registry, fixedStoragePressure{
		snapshot: yagocrawlcontract.StoragePressureSnapshot{
			MeasurementAvailable: true,
		},
	})
	if got := testutil.ToFloat64(metricCollector(
		t,
		registry,
		"storage_pressure_measurement_available",
	)); got != 1 {
		t.Fatalf("available metric = %v", got)
	}
}

func metricsTestBoolMetric(value bool) float64 {
	registry := prometheus.NewRegistry()
	metrics.NewStoragePressureMetrics(registry, fixedStoragePressure{
		snapshot: yagocrawlcontract.StoragePressureSnapshot{Pressured: value},
	})
	families, _ := registry.Gather()
	for _, family := range families {
		if family.GetName() == "storage_pressure" {
			return family.GetMetric()[0].GetGauge().GetValue()
		}
	}

	return -1
}
