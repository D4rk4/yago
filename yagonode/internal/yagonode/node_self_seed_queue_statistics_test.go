package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

type fixedRemoteCrawlPending struct {
	count int
	err   error
}

func (s fixedRemoteCrawlPending) PendingCount(context.Context) (int, error) {
	return s.count, s.err
}

func TestSelfSeedQueueStatisticsReportsBoundedQueueDepths(t *testing.T) {
	source := newSelfSeedQueueStatistics(fixedRemoteCrawlPending{count: 7})
	beforeBind := source.SeedQueueStatistics(t.Context())
	if beforeBind.NoticedKnown || !beforeBind.OfferedKnown || beforeBind.Offered != 7 {
		t.Fatalf("before bind = %#v", beforeBind)
	}
	source.bindCrawlDepth(func(context.Context) (crawlbroker.QueueDepth, error) {
		return crawlbroker.QueueDepth{Pending: 5, Leased: 3}, nil
	})
	statistics := source.SeedQueueStatistics(t.Context())
	if !statistics.NoticedKnown || statistics.Noticed != 8 ||
		!statistics.OfferedKnown || statistics.Offered != 7 {
		t.Fatalf("statistics = %#v", statistics)
	}
}

func TestSelfSeedQueueStatisticsReportsDisabledQueuesAsZero(t *testing.T) {
	source := newSelfSeedQueueStatistics(nil)
	source.bindCrawl(nil)
	statistics := source.SeedQueueStatistics(t.Context())
	if !statistics.NoticedKnown || statistics.Noticed != 0 ||
		!statistics.OfferedKnown || statistics.Offered != 0 {
		t.Fatalf("statistics = %#v", statistics)
	}
}

func TestSelfSeedQueueStatisticsPreservesFailuresAsUnknown(t *testing.T) {
	source := newSelfSeedQueueStatistics(fixedRemoteCrawlPending{err: errors.New("remote")})
	source.bindCrawlDepth(func(context.Context) (crawlbroker.QueueDepth, error) {
		return crawlbroker.QueueDepth{}, errors.New("crawl")
	})
	statistics := source.SeedQueueStatistics(t.Context())
	if statistics.NoticedKnown || statistics.OfferedKnown {
		t.Fatalf("statistics = %#v", statistics)
	}
}
