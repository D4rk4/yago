package crawlermetrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type StoragePressureSource interface {
	Snapshot() yagocrawlcontract.StoragePressureSnapshot
}

func (m *Metrics) RegisterStoragePressure(source StoragePressureSource) {
	m.registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "yacy_crawler_storage_filesystem_available_bytes",
			Help: "Bytes available to the crawler data directory according to the filesystem.",
		}, func() float64 { return float64(source.Snapshot().AvailableBytes) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "yacy_crawler_storage_reserved_free_bytes",
			Help: "Configured free-space reserve for gate-managed crawler growth.",
		}, func() float64 { return float64(source.Snapshot().Policy.ReservedFreeBytes) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "yacy_crawler_storage_pressure_hysteresis_bytes",
			Help: "Additional free space required before crawler storage growth resumes.",
		}, func() float64 {
			return float64(source.Snapshot().Policy.RecoveryHysteresisBytes)
		}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "yacy_crawler_storage_pressure",
			Help: "Whether gate-managed crawler growth and fetch admission are paused.",
		}, func() float64 { return crawlerBoolMetric(source.Snapshot().Pressured) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "yacy_crawler_storage_pressure_measurement_available",
			Help: "Whether the latest crawler filesystem availability measurement succeeded.",
		}, func() float64 {
			return crawlerBoolMetric(source.Snapshot().MeasurementAvailable)
		}),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name: "yacy_crawler_storage_growth_rejections_total",
			Help: "Gate-managed crawler growth admissions deferred by storage pressure.",
		}, func() float64 { return float64(source.Snapshot().RejectedGrowthTotal) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name: "yacy_crawler_storage_pressure_measurement_failures_total",
			Help: "Crawler filesystem availability measurements that failed.",
		}, func() float64 {
			return float64(source.Snapshot().MeasurementFailuresTotal)
		}),
	)
}

func crawlerBoolMetric(value bool) float64 {
	if value {
		return 1
	}

	return 0
}
