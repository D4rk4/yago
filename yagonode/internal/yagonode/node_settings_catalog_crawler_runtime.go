package yagonode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	settingKeyCrawlerFetchWorkers          = "crawler.fetch_workers"
	settingKeyCrawlerMaxPagesPerRun        = "crawler.max_pages_per_run"
	settingKeyPrioritizeAutomaticDiscovery = "crawler.prioritize_automatic_discovery"
)

func crawlerRuntimeDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyCrawlerFetchWorkers,
			title:       "Maximum fetch concurrency per crawler",
			description: "Number of page-fetch workers in each connected yago-crawler process. This does not limit crawl tasks or runs.",
			defaultValue: func(config nodeConfig) string {
				return strconv.Itoa(config.Crawl.FetchWorkers)
			},
			normalize: normalizeFetchWorkerConcurrency,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.FetchWorkers, _ = strconv.Atoi(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				workers, _ := strconv.Atoi(value)
				toggles.ApplyCrawlerFetchWorkers(workers)
			},
		},
		{
			key:         settingKeyCrawlerMaxPagesPerRun,
			title:       "Maximum pages per crawl run",
			description: "Default whole-run page budget for new manual, scheduled, recrawl, swarm-discovery, and web-discovery tasks. Zero is unlimited. Existing runs keep their profile budget.",
			defaultValue: func(config nodeConfig) string {
				return strconv.Itoa(config.Crawl.MaxPagesPerRun)
			},
			normalize: normalizeNonNegativeInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.MaxPagesPerRun, _ = strconv.Atoi(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				maximum, _ := strconv.Atoi(value)
				toggles.ApplyCrawlerMaxPagesPerRun(maximum)
			},
		},
		{
			key:         settingKeyPrioritizeAutomaticDiscovery,
			title:       "Prioritize automatic discovery crawls",
			description: "Serve swarm and web-discovery orders ahead of ordinary orders with a bounded burst so ordinary crawls cannot starve.",
			options:     boolSettingOptions(),
			defaultValue: func(config nodeConfig) string {
				return formatSettingBool(config.Crawl.PrioritizeAutomaticDiscovery)
			},
			normalize: normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.PrioritizeAutomaticDiscovery = value == settingBoolTrue

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				toggles.ApplyAutomaticDiscoveryPriority(value == settingBoolTrue)
			},
		},
	}
}

func normalizeFetchWorkerConcurrency(raw string) (string, error) {
	workers, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || workers < 1 || workers > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		return "", fmt.Errorf(
			"value must be an integer between 1 and %d",
			yagocrawlcontract.MaximumFetchWorkerConcurrency,
		)
	}

	return strconv.Itoa(workers), nil
}
