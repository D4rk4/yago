package yagonode

import (
	"context"
	"errors"
	"testing"
)

type fixedRemoteCrawlPending struct {
	count int
	err   error
}

func (s fixedRemoteCrawlPending) PendingCount(context.Context) (int, error) {
	return s.count, s.err
}

func TestSelfSeedQueueStatisticsLeavesCrawlerFrontierUnknownToNode(t *testing.T) {
	source := newSelfSeedQueueStatistics(fixedRemoteCrawlPending{count: 7})
	statistics := source.SeedQueueStatistics(t.Context())
	if statistics.NoticedKnown ||
		!statistics.OfferedKnown || statistics.Offered != 7 {
		t.Fatalf("statistics = %#v", statistics)
	}
}

func TestSelfSeedQueueStatisticsLeavesDisabledCrawlerFrontierUnknown(t *testing.T) {
	source := newSelfSeedQueueStatistics(nil)
	statistics := source.SeedQueueStatistics(t.Context())
	if statistics.NoticedKnown ||
		!statistics.OfferedKnown || statistics.Offered != 0 {
		t.Fatalf("statistics = %#v", statistics)
	}
}

func TestSelfSeedQueueStatisticsPreservesFailuresAsUnknown(t *testing.T) {
	source := newSelfSeedQueueStatistics(fixedRemoteCrawlPending{err: errors.New("remote")})
	statistics := source.SeedQueueStatistics(t.Context())
	if statistics.NoticedKnown || statistics.OfferedKnown {
		t.Fatalf("statistics = %#v", statistics)
	}
}
