package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

type QueueDepthSource interface {
	CrawlQueueDepth(context.Context) int
	IndexQueueDepth(context.Context) int
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
		func() float64 { return float64(source.CrawlQueueDepth(context.Background())) },
	)
	index := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "queue_index_depth",
			Help: "Fetched documents queued for indexing that are not yet indexed.",
		},
		func() float64 { return float64(source.IndexQueueDepth(context.Background())) },
	)
	registry.MustRegister(crawl, index)

	return &QueueDepthMetrics{crawl: crawl, index: index}
}
