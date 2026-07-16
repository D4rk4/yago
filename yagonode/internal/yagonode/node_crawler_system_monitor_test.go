package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

func TestCrawlerFetchActivitySourcePreservesRuntimeSnapshot(t *testing.T) {
	t.Parallel()

	if newCrawlerFetchActivitySource(nil) != nil {
		t.Fatal("nil crawler registry produced a monitor source")
	}

	activity := crawlerFetchActivityFromSnapshot(crawlbroker.CrawlerRuntimeSnapshot{
		ConnectedCrawlers:      3,
		ActiveFetches:          7,
		ActiveFetchesKnown:     true,
		FetchLimitPerCrawler:   4,
		AggregateFetchCapacity: 12,
	})
	if activity.ConnectedCrawlers != 3 || activity.ActiveFetches != 7 ||
		!activity.ActiveFetchesKnown || activity.FetchLimitPerCrawler != 4 ||
		activity.AggregateFetchCapacity != 12 {
		t.Fatalf("crawler fetch activity = %+v", activity)
	}
}

func TestCrawlerFetchActivitySourceExposesEnabledRuntimeWithoutWorkers(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	source := newCrawlerFetchActivitySource(crawlControlRegistry(runtime))
	if source == nil {
		t.Fatal("enabled crawler runtime produced no monitor source")
	}

	activity := source.CrawlerFetchActivity()
	if activity.ConnectedCrawlers != 0 || activity.ActiveFetches != 0 ||
		!activity.ActiveFetchesKnown ||
		activity.FetchLimitPerCrawler != yagocrawlcontract.DefaultFetchWorkerConcurrency ||
		activity.AggregateFetchCapacity != 0 {
		t.Fatalf("idle enabled crawler activity = %+v", activity)
	}
}
