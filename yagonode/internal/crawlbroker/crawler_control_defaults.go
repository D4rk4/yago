package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func crawlerControlDefaultsFor(cfg Config) (crawlerControlDefaults, error) {
	fetchWorkers := cfg.FetchWorkers
	if fetchWorkers <= 0 {
		fetchWorkers = yagocrawlcontract.DefaultFetchWorkerConcurrency
	}
	maximumActiveRuns := cfg.MaximumActiveRuns
	if maximumActiveRuns <= 0 {
		maximumActiveRuns = yagocrawlcontract.DefaultActiveCrawlRunConcurrency
	}
	processPagesPerSecond := cfg.ProcessPagesPerSecond
	if processPagesPerSecond < 0 ||
		processPagesPerSecond > yagocrawlcontract.MaximumProcessPagesPerSecond {
		processPagesPerSecond = yagocrawlcontract.DefaultProcessPagesPerSecond
	}
	maximumRedirects := cfg.MaximumRedirects
	if maximumRedirects < 0 || maximumRedirects > yagocrawlcontract.MaximumPageRedirects {
		maximumRedirects = yagocrawlcontract.DefaultMaximumPageRedirects
	}
	runtimePolicy := cfg.RuntimePolicy
	if runtimePolicy.UserAgent == "" {
		runtimePolicy = yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	}
	if err := runtimePolicy.Validate(); err != nil {
		return crawlerControlDefaults{}, fmt.Errorf("validate crawler runtime policy: %w", err)
	}

	return crawlerControlDefaults{
		fetchWorkers:                 uint32(fetchWorkers),
		processPagesPerSecond:        uint32(processPagesPerSecond),
		processRateSet:               true,
		maximumRedirects:             uint32(maximumRedirects),
		maximumRedirectsSet:          true,
		maximumActiveRuns:            uint32(maximumActiveRuns),
		prioritizeAutomaticDiscovery: !cfg.DisableAutomaticDiscoveryPriority,
		storagePressurePolicy:        cfg.StoragePressurePolicy,
		runtimePolicy:                runtimePolicy,
	}, nil
}
