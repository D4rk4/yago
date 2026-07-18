package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type StoragePressureSource interface {
	Snapshot() yagocrawlcontract.StoragePressureSnapshot
}

type StoragePressureMetrics struct {
	available           prometheus.GaugeFunc
	reservedFree        prometheus.GaugeFunc
	recoveryHysteresis  prometheus.GaugeFunc
	pressured           prometheus.GaugeFunc
	measurement         prometheus.GaugeFunc
	rejectedGrowth      prometheus.CounterFunc
	measurementFailures prometheus.CounterFunc
}

func NewStoragePressureMetrics(
	registry prometheus.Registerer,
	source StoragePressureSource,
) *StoragePressureMetrics {
	available := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "storage_filesystem_available_bytes",
		Help: "Bytes available to the node data directory according to the filesystem.",
	}, func() float64 { return float64(source.Snapshot().AvailableBytes) })
	reservedFree := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "storage_reserved_free_bytes",
		Help: "Configured free-space reserve for gate-managed node growth.",
	}, func() float64 { return float64(source.Snapshot().Policy.ReservedFreeBytes) })
	recoveryHysteresis := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "storage_pressure_hysteresis_bytes",
		Help: "Additional free space required before node-side storage growth resumes.",
	}, func() float64 { return float64(source.Snapshot().Policy.RecoveryHysteresisBytes) })
	pressured := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "storage_pressure",
		Help: "Whether gate-managed node growth is paused by storage pressure.",
	}, func() float64 { return boolMetric(source.Snapshot().Pressured) })
	measurement := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "storage_pressure_measurement_available",
		Help: "Whether the latest node filesystem availability measurement succeeded.",
	}, func() float64 { return boolMetric(source.Snapshot().MeasurementAvailable) })
	rejectedGrowth := prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "storage_growth_rejections_total",
		Help: "Gate-managed node growth admissions rejected by storage pressure.",
	}, func() float64 { return float64(source.Snapshot().RejectedGrowthTotal) })
	measurementFailures := prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "storage_pressure_measurement_failures_total",
		Help: "Node filesystem availability measurements that failed.",
	}, func() float64 { return float64(source.Snapshot().MeasurementFailuresTotal) })
	registry.MustRegister(
		available,
		reservedFree,
		recoveryHysteresis,
		pressured,
		measurement,
		rejectedGrowth,
		measurementFailures,
	)

	return &StoragePressureMetrics{
		available: available, reservedFree: reservedFree,
		recoveryHysteresis: recoveryHysteresis, pressured: pressured,
		measurement: measurement, rejectedGrowth: rejectedGrowth,
		measurementFailures: measurementFailures,
	}
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}

	return 0
}
