package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

func TestCrawlQueueDepthSourceOutstanding(t *testing.T) {
	ctx := context.Background()

	if got := (crawlQueueDepthSource{}).outstanding(ctx); got != 0 {
		t.Fatalf("nil probe depth = %d, want 0", got)
	}

	live := crawlQueueDepthSource{
		probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{Pending: 6, Leased: 4}, nil
		},
	}
	if got := live.outstanding(ctx); got != 10 {
		t.Fatalf("live depth = %d, want 10", got)
	}

	failing := crawlQueueDepthSource{
		probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{}, errors.New("boom")
		},
	}
	if got := failing.outstanding(ctx); got != 0 {
		t.Fatalf("failing depth = %d, want 0", got)
	}
}

type depthProbeCrawl struct {
	*recordingCrawl
	depth crawlbroker.QueueDepth
	err   error
}

func (d *depthProbeCrawl) crawlQueueDepth(context.Context) (crawlbroker.QueueDepth, error) {
	return d.depth, d.err
}

func TestCrawlQueueProbe(t *testing.T) {
	if crawlQueueProbe(nil) != nil {
		t.Fatal("nil runtime must yield nil probe")
	}
	if crawlQueueProbe(&recordingCrawl{}) != nil {
		t.Fatal("runtime without depth accessor must yield nil probe")
	}

	runtime := &depthProbeCrawl{
		recordingCrawl: &recordingCrawl{},
		depth:          crawlbroker.QueueDepth{Pending: 2, Leased: 1},
	}
	probe := crawlQueueProbe(runtime)
	if probe == nil {
		t.Fatal("crawl runtime must yield a probe")
	}
	depth, err := probe(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if depth.Outstanding() != 3 {
		t.Fatalf("probe depth = %+v, want outstanding 3", depth)
	}
}
