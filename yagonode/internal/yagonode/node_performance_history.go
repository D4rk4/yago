package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func newNodePerformanceHistory(
	endpoints *metrics.HTTPEndpointMetrics,
) *metrichistory.Sampler {
	return metrichistory.New(
		endpoints.Registry(),
		performanceHistoryCapacity,
		currentHostMemory,
	)
}
