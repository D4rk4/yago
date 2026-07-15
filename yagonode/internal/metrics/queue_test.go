package metrics

import (
	"context"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type stubQueue struct {
	crawl, index           int
	crawlKnown, indexKnown bool
}

func (s stubQueue) CrawlQueueDepth(context.Context) (int, bool) {
	return s.crawl, s.crawlKnown
}

func (s stubQueue) IndexQueueDepth(context.Context) (int, bool) {
	return s.index, s.indexKnown
}

func TestQueueDepthReportsSourceDepths(t *testing.T) {
	queue := NewQueueDepthMetrics(prometheus.NewRegistry(), stubQueue{
		crawl: 5, index: 2, crawlKnown: true, indexKnown: true,
	})

	if got := testutil.ToFloat64(queue.crawl); got != 5 {
		t.Errorf("crawl depth = %v, want 5", got)
	}
	if got := testutil.ToFloat64(queue.index); got != 2 {
		t.Errorf("index depth = %v, want 2", got)
	}
}

func TestQueueDepthReportsUnavailableAndClampsNegativeValues(t *testing.T) {
	queue := NewQueueDepthMetrics(prometheus.NewRegistry(), stubQueue{crawl: -2, crawlKnown: true})

	if got := testutil.ToFloat64(queue.crawl); got != 0 {
		t.Errorf("negative crawl depth = %v, want 0", got)
	}
	if got := testutil.ToFloat64(queue.index); !math.IsNaN(got) {
		t.Errorf("unknown index depth = %v, want NaN", got)
	}
}
