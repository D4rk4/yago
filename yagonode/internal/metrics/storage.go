package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

type StorageCapacity interface {
	QuotaBytes() int64
	UsedBytes(context.Context) (int64, error)
}

type StorageMetrics struct {
	quota prometheus.GaugeFunc
	used  prometheus.GaugeFunc
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
	registry.MustRegister(quota, used)

	return &StorageMetrics{quota: quota, used: used}
}
