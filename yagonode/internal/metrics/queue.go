package metrics

import (
	"context"
	"math"

	"github.com/prometheus/client_golang/prometheus"
)

type QueueDepthSource interface {
	CrawlQueueDepth(context.Context) (int, bool)
	IndexQueueDepth(context.Context) (int, bool)
}

type QueueDepthMetrics struct {
	crawl prometheus.GaugeFunc
	index prometheus.GaugeFunc
}

func NewQueueDepthMetrics(
	registry prometheus.Registerer,
	source QueueDepthSource,
) *QueueDepthMetrics {
	crawl := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "queue_crawl_depth",
			Help: "URLs queued for crawling that are not yet fetched.",
		},
		func() float64 {
			depth, known := source.CrawlQueueDepth(context.Background())
			if !known {
				return math.NaN()
			}

			return float64(max(0, depth))
		},
	)
	index := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "queue_index_depth",
			Help: "Fetched documents queued for indexing that are not yet indexed.",
		},
		func() float64 {
			depth, known := source.IndexQueueDepth(context.Background())
			if !known {
				return math.NaN()
			}

			return float64(max(0, depth))
		},
	)
	registry.MustRegister(crawl, index)

	return &QueueDepthMetrics{crawl: crawl, index: index}
}
