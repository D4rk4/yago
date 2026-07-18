package main

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlerControlsKeepFetchAndActiveRunConcurrencyIndependent(t *testing.T) {
	concurrency := newWorkerConcurrency(1)
	maximumActiveRuns := 1
	crawlFrontier := frontier.NewFrontier(4, nil)
	handler := assembleCrawlerControlHandler(
		func() {},
		concurrency,
		func(maximum int) { maximumActiveRuns = maximum },
		crawlFrontier,
	)
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:         yagocrawlcontract.CrawlControlSetWorkers,
		FetchWorkers: 2,
	})
	if got := concurrency.Current(); got != 2 {
		t.Fatalf("fetch workers = %d, want 2", got)
	}
	if maximumActiveRuns != 1 {
		t.Fatalf("fetch-worker directive changed active-run maximum to %d", maximumActiveRuns)
	}
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
		MaximumActiveRuns: 2,
	})
	if maximumActiveRuns != 2 {
		t.Fatalf("active-run directive left maximum at %d, want 2", maximumActiveRuns)
	}
}
