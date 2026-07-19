package yagonode

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func applyCrawlerFacilityBootstrap(
	getenv func(string) string,
	policy *yagocrawlcontract.CrawlerRuntimePolicy,
) error {
	browserPath, err := yagocrawlcontract.ParseCrawlerBrowserPath(
		getenv(envCrawlerBrowserPath),
	)
	if err != nil {
		return fmt.Errorf("%s: %w", envCrawlerBrowserPath, err)
	}
	metricsAddress, err := yagocrawlcontract.ParseCrawlerMetricsAddress(
		getenv(envCrawlerMetricsAddress),
	)
	if err != nil {
		return fmt.Errorf("%s: %w", envCrawlerMetricsAddress, err)
	}
	policy.BrowserPath = browserPath
	policy.MetricsAddress = metricsAddress

	return nil
}
