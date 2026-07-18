package metrics

import (
	"context"
	"math"

	"github.com/prometheus/client_golang/prometheus"
)

type CrawlStateCapacity interface {
	UsedBytes(context.Context) (int64, error)
}

type CrawlStateMetrics struct {
	used      prometheus.GaugeFunc
	highWater prometheus.GaugeFunc
}

func NewCrawlStateMetrics(
	registry prometheus.Registerer,
	capacity CrawlStateCapacity,
	highWater func() (int64, error),
) *CrawlStateMetrics {
	used := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "crawl_broker_state_used_bytes",
			Help: "Live durable crawl broker state in bytes.",
		},
		func() float64 {
			bytes, err := capacity.UsedBytes(context.Background())
			if err != nil {
				return math.NaN()
			}

			return float64(bytes)
		},
	)
	highWaterGauge := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "crawl_broker_state_file_bytes",
			Help: "Allocated crawl broker database file size in bytes.",
		},
		func() float64 {
			bytes, err := highWater()
			if err != nil {
				return math.NaN()
			}

			return float64(bytes)
		},
	)
	registry.MustRegister(used, highWaterGauge)

	return &CrawlStateMetrics{used: used, highWater: highWaterGauge}
}
