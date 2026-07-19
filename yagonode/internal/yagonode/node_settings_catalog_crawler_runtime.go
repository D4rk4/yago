package yagonode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	settingKeyCrawlerFetchWorkers          = "crawler.fetch_workers"
	settingKeyCrawlerProcessPagesPerSecond = "crawler.max_pages_per_second"
	settingKeyCrawlerMaximumRedirects      = "crawler.max_redirects"
	settingKeyCrawlerMaximumActiveRuns     = "crawler.max_active_runs"
	settingKeyCrawlerMaxPagesPerRun        = "crawler.max_pages_per_run"
	settingKeyPrioritizeAutomaticDiscovery = "crawler.prioritize_automatic_discovery"
)

func crawlerRuntimeDefinitions() []settingDefinition {
	return []settingDefinition{
		crawlerFetchWorkerDefinition(),
		crawlerFleetRateDefinition(),
		crawlerMaximumRedirectDefinition(),
		crawlerMaximumActiveRunDefinition(),
		crawlerMaximumPageBudgetDefinition(),
		crawlerAutomaticDiscoveryPriorityDefinition(),
	}
}

func crawlerFetchWorkerDefinition() settingDefinition {
	return settingDefinition{
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
	}
}

func crawlerFleetRateDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerProcessPagesPerSecond,
		title:       "Maximum fleet-wide fetch-start rate",
		description: "Maximum page-fetch starts per second across all connected yago-crawler processes and active crawl tasks. Zero is unlimited. Per-process smoothing, per-host politeness, and per-run rates remain additional limits.",
		defaultValue: func(config nodeConfig) string {
			return strconv.Itoa(config.Crawl.ProcessPagesPerSecond)
		},
		normalize: normalizeProcessPagesPerSecond,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.Crawl.ProcessPagesPerSecond, _ = strconv.Atoi(value)

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			pagesPerSecond, _ := strconv.Atoi(value)
			toggles.ApplyCrawlerProcessPagesPerSecond(pagesPerSecond)
		},
	}
}

func crawlerMaximumRedirectDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerMaximumRedirects,
		title:       "Maximum redirects per page",
		description: "Maximum HTTP redirect hops for each fast-path or browser page fetch. Zero rejects the first redirect. Connected crawlers apply changes live and relaunch browser sessions before their next render.",
		defaultValue: func(config nodeConfig) string {
			return strconv.Itoa(config.Crawl.MaximumRedirects)
		},
		normalize: normalizeMaximumRedirects,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.Crawl.MaximumRedirects, _ = strconv.Atoi(value)

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			maximum, _ := strconv.Atoi(value)
			toggles.ApplyCrawlerMaximumRedirects(maximum)
		},
	}
}

func crawlerMaximumActiveRunDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerMaximumActiveRuns,
		title:       "Maximum active crawl tasks",
		description: "Number of distinct crawl tasks each connected yago-crawler process may keep active. Additional recovered and new tasks wait without consuming frontier or progress capacity.",
		defaultValue: func(config nodeConfig) string {
			return strconv.Itoa(config.Crawl.MaxActiveRuns)
		},
		normalize: normalizeActiveCrawlRunConcurrency,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.Crawl.MaxActiveRuns, _ = strconv.Atoi(value)

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			maximum, _ := strconv.Atoi(value)
			toggles.ApplyCrawlerMaximumActiveRuns(maximum)
		},
	}
}

func crawlerMaximumPageBudgetDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerMaxPagesPerRun,
		title:       "Maximum pages per crawl run",
		description: "Default whole-run page budget for new manual and scheduled tasks. A positive value can only reduce the separate swarm and web-discovery task caps; zero leaves those dedicated caps intact. Existing manual runs keep their profile budget; recovered legacy automatic runs derive their whole-run cap from the stored per-host cap.",
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
	}
}

func crawlerAutomaticDiscoveryPriorityDefinition() settingDefinition {
	return settingDefinition{
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

func normalizeProcessPagesPerSecond(raw string) (string, error) {
	pagesPerSecond, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || pagesPerSecond < 0 ||
		pagesPerSecond > yagocrawlcontract.MaximumProcessPagesPerSecond {
		return "", fmt.Errorf(
			"value must be an integer between 0 and %d",
			yagocrawlcontract.MaximumProcessPagesPerSecond,
		)
	}

	return strconv.Itoa(pagesPerSecond), nil
}

func normalizeMaximumRedirects(raw string) (string, error) {
	maximum, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || maximum < 0 || maximum > yagocrawlcontract.MaximumPageRedirects {
		return "", fmt.Errorf(
			"value must be an integer between 0 and %d",
			yagocrawlcontract.MaximumPageRedirects,
		)
	}

	return strconv.Itoa(maximum), nil
}

func normalizeActiveCrawlRunConcurrency(raw string) (string, error) {
	maximum, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || maximum < 1 ||
		maximum > yagocrawlcontract.MaximumActiveCrawlRunConcurrency {
		return "", fmt.Errorf(
			"value must be an integer between 1 and %d",
			yagocrawlcontract.MaximumActiveCrawlRunConcurrency,
		)
	}

	return strconv.Itoa(maximum), nil
}
