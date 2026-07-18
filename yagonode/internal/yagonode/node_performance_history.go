package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func attachPerformanceHistory(
	ctx context.Context,
	endpoints *metrics.HTTPEndpointMetrics,
	sources *consoleAdminSources,
) func() {
	history := metrichistory.New(
		endpoints.Registry(),
		performanceHistoryCapacity,
		currentHostMemory,
	)
	sources.perfHistory = newPerformanceHistorySource(history)

	return startPerformanceHistorySampler(ctx, history)
}
