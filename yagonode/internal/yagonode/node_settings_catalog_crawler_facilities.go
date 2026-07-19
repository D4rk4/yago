package yagonode

import "github.com/D4rk4/yago/yagocrawlcontract"

const (
	settingKeyCrawlerBrowserPath    = "crawler.browser_path"
	settingKeyCrawlerMetricsAddress = "crawler.metrics_address"
)

func crawlerProcessFacilityDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:   settingKeyCrawlerBrowserPath,
			title: "Firefox executable path",
			description: "Absolute firefox or firefox-esr launcher path. Empty uses PATH " +
				"discovery. Crawlers require a trusted root-owned path and restart automatically.",
			defaultValue: func(config nodeConfig) string {
				return config.Crawl.RuntimePolicy.BrowserPath
			},
			normalize: yagocrawlcontract.ParseCrawlerBrowserPath,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.RuntimePolicy.BrowserPath = value

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				updateCrawlerRuntimePolicy(
					toggles,
					func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
						policy.BrowserPath = value
					},
				)
			},
		},
		{
			key:   settingKeyCrawlerMetricsAddress,
			title: "Crawler metrics listen address",
			description: "Optional loopback IP-literal listener for crawler Prometheus metrics. " +
				"Empty disables the listener. Connected crawlers restart automatically.",
			defaultValue: func(config nodeConfig) string {
				return config.Crawl.RuntimePolicy.MetricsAddress
			},
			normalize: yagocrawlcontract.ParseCrawlerMetricsAddress,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.RuntimePolicy.MetricsAddress = value

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				updateCrawlerRuntimePolicy(
					toggles,
					func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
						policy.MetricsAddress = value
					},
				)
			},
		},
	}
}
