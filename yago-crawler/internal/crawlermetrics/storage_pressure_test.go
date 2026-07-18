package crawlermetrics_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type fixedStoragePressureSource struct {
	snapshot yagocrawlcontract.StoragePressureSnapshot
}

func (source fixedStoragePressureSource) Snapshot() yagocrawlcontract.StoragePressureSnapshot {
	return source.snapshot
}

func TestMetricsExposeCrawlerStoragePressure(t *testing.T) {
	metrics := crawlermetrics.New()
	metrics.RegisterStoragePressure(fixedStoragePressureSource{
		snapshot: yagocrawlcontract.StoragePressureSnapshot{
			AvailableBytes: 31,
			Policy: yagocrawlcontract.StoragePressurePolicy{
				ReservedFreeBytes:       29,
				RecoveryHysteresisBytes: 7,
			},
			Pressured:                true,
			MeasurementAvailable:     false,
			RejectedGrowthTotal:      5,
			MeasurementFailuresTotal: 3,
		},
	})
	body := scrapeMetrics(t, metrics)
	for _, want := range []string{
		"yacy_crawler_storage_filesystem_available_bytes 31",
		"yacy_crawler_storage_reserved_free_bytes 29",
		"yacy_crawler_storage_pressure_hysteresis_bytes 7",
		"yacy_crawler_storage_pressure 1",
		"yacy_crawler_storage_pressure_measurement_available 0",
		"yacy_crawler_storage_growth_rejections_total 5",
		"yacy_crawler_storage_pressure_measurement_failures_total 3",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q in:\n%s", want, body)
		}
	}
}
