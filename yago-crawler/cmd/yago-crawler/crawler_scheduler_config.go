package main

import (
	"fmt"

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
