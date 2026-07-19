package main

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlerControlsKeepFetchAndActiveRunConcurrencyIndependent(t *testing.T) {
	concurrency := newWorkerConcurrency(1)
	maximumActiveRuns := 1
	processRate := uint32(1)
	maximumRedirects := 10
	crawlFrontier := frontier.NewFrontier(4, nil)
	handler := assembleCrawlerControlHandler(crawlerControlActions{
		restart:             func() {},
		concurrency:         concurrency,
		setProcessRate:      func(rate uint32) { processRate = rate },
		setMaximumRedirects: func(maximum int) { maximumRedirects = maximum },
		resizeActiveRuns:    func(maximum int) { maximumActiveRuns = maximum },
		frontier:            crawlFrontier,
	})
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:         yagocrawlcontract.CrawlControlSetWorkers,
		FetchWorkers: 2,
	})
	if got := concurrency.Current(); got != 2 {
		t.Fatalf("fetch workers = %d, want 2", got)
	}
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
		MaximumRedirects: 4,
	})
	if maximumRedirects != 4 {
		t.Fatalf("maximum redirects = %d, want 4", maximumRedirects)
	}
	if maximumActiveRuns != 1 {
		t.Fatalf("fetch-worker directive changed active-run maximum to %d", maximumActiveRuns)
	}
	if processRate != 1 {
		t.Fatalf("fetch-worker directive changed process rate to %d", processRate)
	}
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
		ProcessPagesPerSecond: 19,
	})
	if processRate != 19 {
		t.Fatalf("process rate = %d, want 19", processRate)
	}
	if got := concurrency.Current(); got != 2 {
		t.Fatalf("process-rate directive changed fetch workers to %d", got)
	}
	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
		MaximumActiveRuns: 2,
	})
	if maximumActiveRuns != 2 {
		t.Fatalf("active-run directive left maximum at %d, want 2", maximumActiveRuns)
	}
}
