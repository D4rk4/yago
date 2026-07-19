package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func loadCrawlSchedulerConfig(
	getenv func(string) string,
	crawl CrawlConfig,
) (CrawlConfig, error) {
	workers, err := envPositiveInt(getenv, EnvWorkers, crawl.Workers)
	if err != nil {
		return CrawlConfig{}, err
	}
	if workers > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		return CrawlConfig{}, fmt.Errorf(
			"%s: must not exceed %d",
			EnvWorkers,
			yagocrawlcontract.MaximumFetchWorkerConcurrency,
		)
	}
	crawl.Workers = workers
	processPagesPerSecondRaw := strings.TrimSpace(getenv(EnvProcessPagesPerSecond))
	if processPagesPerSecondRaw == "" {
		processPagesPerSecondRaw = strconv.FormatUint(uint64(crawl.ProcessPagesPerSecond), 10)
	}
	processPagesPerSecond, err := yagocrawlcontract.ParseProcessPagesPerSecond(
		processPagesPerSecondRaw,
	)
	if err != nil {
		return CrawlConfig{}, fmt.Errorf("%s: %w", EnvProcessPagesPerSecond, err)
	}
	crawl.ProcessPagesPerSecond = processPagesPerSecond
	maximumActiveRuns, err := envPositiveInt(
		getenv,
		EnvMaxActiveRuns,
		crawl.MaxActiveRuns,
	)
	if err != nil {
		return CrawlConfig{}, err
	}
	if maximumActiveRuns > yagocrawlcontract.MaximumActiveCrawlRunConcurrency {
		return CrawlConfig{}, fmt.Errorf(
			"%s: must not exceed %d",
			EnvMaxActiveRuns,
			yagocrawlcontract.MaximumActiveCrawlRunConcurrency,
		)
	}
	crawl.MaxActiveRuns = maximumActiveRuns

	priority, err := envBool(
		getenv,
		EnvPrioritizeAutomaticDiscovery,
		crawl.PrioritizeAutomaticDiscovery,
	)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.PrioritizeAutomaticDiscovery = priority

	maxHostConcurrency, err := envPositiveInt(
		getenv,
		EnvMaxHostConcurrency,
		crawl.MaxHostConcurrency,
	)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.MaxHostConcurrency = maxHostConcurrency

	return crawl, nil
}
