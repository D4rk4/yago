package metrics

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type stubQueue struct{ crawl, index int }

func (s stubQueue) CrawlQueueDepth(context.Context) int { return s.crawl }

func (s stubQueue) IndexQueueDepth(context.Context) int { return s.index }

func TestQueueDepthReportsSourceDepths(t *testing.T) {
	queue := NewQueueDepthMetrics(prometheus.NewRegistry(), stubQueue{crawl: 5, index: 2})

	if got := testutil.ToFloat64(queue.crawl); got != 5 {
		t.Errorf("crawl depth = %v, want 5", got)
	}
	if got := testutil.ToFloat64(queue.index); got != 2 {
		t.Errorf("index depth = %v, want 2", got)
	}
}
