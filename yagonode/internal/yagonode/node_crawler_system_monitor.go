package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

type crawlerFetchActivitySource struct {
	registry *crawlbroker.ControlRegistry
}

func newCrawlerFetchActivitySource(
	registry *crawlbroker.ControlRegistry,
) adminui.CrawlerFetchActivitySource {
	if registry == nil {
		return nil
	}

	return crawlerFetchActivitySource{registry: registry}
}

func (s crawlerFetchActivitySource) CrawlerFetchActivity() adminui.CrawlerFetchActivity {
	return crawlerFetchActivityFromSnapshot(s.registry.RuntimeSnapshot())
}

func crawlerFetchActivityFromSnapshot(
	snapshot crawlbroker.CrawlerRuntimeSnapshot,
) adminui.CrawlerFetchActivity {
	return adminui.CrawlerFetchActivity{
		ConnectedCrawlers:      snapshot.ConnectedCrawlers,
		ActiveFetches:          snapshot.ActiveFetches,
		FetchLimitPerCrawler:   snapshot.FetchLimitPerCrawler,
		AggregateFetchCapacity: snapshot.AggregateFetchCapacity,
		ActiveFetchesKnown:     snapshot.ActiveFetchesKnown,
	}
}
