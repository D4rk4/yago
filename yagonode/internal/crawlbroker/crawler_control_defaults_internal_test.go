package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlerControlDefaultsNormalizeBootstrapValues(t *testing.T) {
	defaults, err := crawlerControlDefaultsFor(Config{
		ProcessPagesPerSecond: -1,
		MaximumRedirects:      yagocrawlcontract.MaximumPageRedirects + 1,
	})
	if err != nil {
		t.Fatalf("default controls: %v", err)
	}
	if defaults.fetchWorkers != yagocrawlcontract.DefaultFetchWorkerConcurrency ||
		defaults.maximumActiveRuns != yagocrawlcontract.DefaultActiveCrawlRunConcurrency ||
		defaults.processPagesPerSecond != yagocrawlcontract.DefaultProcessPagesPerSecond ||
		defaults.maximumRedirects != yagocrawlcontract.DefaultMaximumPageRedirects ||
		defaults.runtimePolicy.UserAgent == "" {
		t.Fatalf("default controls = %+v", defaults)
	}

	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.UserAgent = "custom-agent"
	configured, err := crawlerControlDefaultsFor(Config{
		FetchWorkers:                      8,
		ProcessPagesPerSecond:             12,
		MaximumRedirects:                  4,
		MaximumActiveRuns:                 16,
		DisableAutomaticDiscoveryPriority: true,
		RuntimePolicy:                     policy,
	})
	if err != nil {
		t.Fatalf("configured controls: %v", err)
	}
	if configured.fetchWorkers != 8 || configured.processPagesPerSecond != 12 ||
		configured.maximumRedirects != 4 || configured.maximumActiveRuns != 16 ||
		configured.prioritizeAutomaticDiscovery ||
		configured.runtimePolicy.UserAgent != "custom-agent" {
		t.Fatalf("configured controls = %+v", configured)
	}

	policy.MaximumDepth = 0
	if _, err := crawlerControlDefaultsFor(Config{RuntimePolicy: policy}); err == nil {
		t.Fatal("invalid runtime policy was accepted")
	}
}
