package yagonode

import (
	"math"
	"strconv"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const settingKeyCrawlerFrontierStateMaximumBytes = "crawler.frontier_state_max_bytes"

func crawlerFrontierStateMaximumDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerFrontierStateMaximumBytes,
		title:       "Crawler frontier-state soft ceiling",
		description: "Pause new crawl growth when crawler/frontier-v1.db reaches this physical size. Lifecycle, settlement, and recovery writes continue. Crawler startup attempts to reclaim unused bbolt pages before opening the file; the value is a soft admission boundary, not a filesystem quota.",
		defaultValue: func(config nodeConfig) string {
			return formatCrawlerFrontierStateMaximumBytes(
				config.Crawl.RuntimePolicy.FrontierStateMaximumBytes,
			)
		},
		normalize: normalizeStoragePressureSize,
		apply: func(config nodeConfig, value string) nodeConfig {
			maximumBytes, _ := parseCrawlerFrontierStateMaximumBytes(value)
			config.Crawl.RuntimePolicy.FrontierStateMaximumBytes = maximumBytes

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			maximumBytes, _ := parseCrawlerFrontierStateMaximumBytes(value)
			updateCrawlerRuntimePolicy(
				toggles,
				func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
					policy.FrontierStateMaximumBytes = maximumBytes
				},
			)
		},
	}
}

func formatCrawlerFrontierStateMaximumBytes(maximumBytes uint64) string {
	if maximumBytes > math.MaxInt64 {
		return strconv.FormatUint(maximumBytes, 10) + "B"
	}

	return formatByteSize(int64(maximumBytes))
}

func parseCrawlerFrontierStateMaximumBytes(raw string) (uint64, error) {
	maximumBytes, err := parseByteSize(raw)
	if err != nil {
		return 0, err
	}
	if maximumBytes <= 0 {
		return 0, nil
	}

	return uint64(maximumBytes), nil
}
