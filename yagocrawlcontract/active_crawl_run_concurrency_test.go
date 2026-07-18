package yagocrawlcontract

import "testing"

func TestActiveCrawlRunConcurrencyPreservesIndependentOperationalBounds(t *testing.T) {
	if DefaultActiveCrawlRunConcurrency != 32 ||
		MaximumActiveCrawlRunConcurrency != 256 ||
		DefaultActiveCrawlRunConcurrency <= DefaultFetchWorkerConcurrency ||
		MaximumActiveCrawlRunConcurrency > MaximumHeartbeatActiveLeases {
		t.Fatalf(
			"active crawl run concurrency = %d/%d",
			DefaultActiveCrawlRunConcurrency,
			MaximumActiveCrawlRunConcurrency,
		)
	}
}
