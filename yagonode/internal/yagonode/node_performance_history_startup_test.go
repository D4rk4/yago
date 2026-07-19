package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

type startupQueueDepth struct {
	crawl int
	index int
}

func (depth startupQueueDepth) CrawlQueueDepth(context.Context) (int, bool) {
	return depth.crawl, true
}

func (depth startupQueueDepth) IndexQueueDepth(context.Context) (int, bool) {
	return depth.index, true
}

func TestPerformanceHistoryFirstSampleIncludesRegisteredQueueGauges(t *testing.T) {
	endpoints := metrics.NewHTTPEndpointMetrics()
	history := newNodePerformanceHistory(endpoints)
	source := newPerformanceHistorySource(history)
	metrics.NewQueueDepthMetrics(endpoints.Registry(), startupQueueDepth{crawl: 17, index: 9})
	stop := startPerformanceHistorySampler(t.Context(), history)
	t.Cleanup(stop)

	wants := map[string]float64{
		metrichistory.SeriesCrawlQueue: 17,
		metrichistory.SeriesIndexQueue: 9,
	}
	for _, series := range source.Series() {
		want, ok := wants[series.Name]
		if !ok {
			continue
		}
		if len(series.Points) != 1 || series.Points[0].Value != want {
			t.Errorf("startup %s = %+v, want one point of %v", series.Name, series.Points, want)
		}
		delete(wants, series.Name)
	}
	if len(wants) != 0 {
		t.Fatalf("missing startup queue series: %v", wants)
	}
}
