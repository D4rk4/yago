package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type StorageCapacity interface {
	QuotaBytes() int64
	UsedBytes(context.Context) (int64, error)
	// ReadDeferred reports the cumulative time ingest writes have yielded to
	// in-flight interactive reads (IO-PRIO-01 / PERF-PRIO-02).
	ReadDeferred() time.Duration
}

type StorageMetrics struct {
	quota     prometheus.GaugeFunc
	used      prometheus.GaugeFunc
	readDefer prometheus.CounterFunc
}

func NewStorageMetrics(registry prometheus.Registerer, capacity StorageCapacity) *StorageMetrics {
	quota := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "storage_quota_bytes",
			Help: "Configured storage quota in bytes.",
		},
		func() float64 { return float64(capacity.QuotaBytes()) },
	)
	used := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "storage_used_bytes",
			Help: "Storage currently used in bytes.",
		},
		func() float64 {
			bytes, err := capacity.UsedBytes(context.Background())
			if err != nil {
				return 0
			}

			return float64(bytes)
		},
	)
	readDefer := prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name: "storage_read_defer_seconds_total",
			Help: "Cumulative seconds ingest writes yielded to interactive reads (IO-PRIO-01).",
		},
		func() float64 { return capacity.ReadDeferred().Seconds() },
	)
	registry.MustRegister(quota, used, readDefer)

	return &StorageMetrics{quota: quota, used: used, readDefer: readDefer}
}
