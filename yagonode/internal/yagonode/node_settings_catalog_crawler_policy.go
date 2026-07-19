package yagonode

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	settingKeyCrawlerAllowPrivateNetworks = "crawler.allow_private_networks"
	settingKeyCrawlerAllowCIDRs           = "crawler.allow_cidrs"
	settingKeyCrawlerBrowserSandbox       = "crawler.browser_sandbox"
	settingKeyCrawlerBrowserFailureLimit  = "crawler.browser_failure_threshold"
	settingKeyCrawlerConnectTimeout       = "crawler.connect_timeout"
	settingKeyCrawlerCrawlDelay           = "crawler.crawl_delay"
	settingKeyCrawlerHeaderTimeout        = "crawler.header_timeout"
	settingKeyCrawlerMaximumDepth         = "crawler.max_depth"
	settingKeyCrawlerMaximumHostFetches   = "crawler.max_host_concurrency"
	settingKeyCrawlerRequestTimeout       = "crawler.request_timeout"
	settingKeyCrawlerRunPagesPerMinute    = "crawler.run_pages_per_minute"
	settingKeyCrawlerSitemapURLLimit      = "crawler.sitemap_url_limit"
	settingKeyCrawlerTLSTimeout           = "crawler.tls_timeout"
	settingKeyCrawlerShutdownGrace        = "crawler.shutdown_grace"
	settingKeyCrawlerUserAgent            = "crawler.http.user_agent"
)

func crawlerRuntimePolicyDefinitions() []settingDefinition {
	definitions := make([]settingDefinition, 0, 17)
	definitions = append(definitions,
		crawlerAllowPrivateNetworksDefinition(),
		crawlerAllowedPrivateCIDRsDefinition(),
		crawlerBrowserSandboxDefinition(),
		crawlerBrowserFailureThresholdDefinition(),
		crawlerConnectTimeoutDefinition(),
		crawlerCrawlDelayDefinition(),
		crawlerFrontierStateMaximumDefinition(),
		crawlerHeaderTimeoutDefinition(),
		crawlerMaximumDepthDefinition(),
		crawlerMaximumHostConcurrencyDefinition(),
		crawlerRequestTimeoutDefinition(),
		crawlerRunPagesPerMinuteDefinition(),
		crawlerSitemapURLLimitDefinition(),
		crawlerTLSTimeoutDefinition(),
		crawlerShutdownGraceDefinition(),
		crawlerUserAgentDefinition(),
	)

	return append(definitions, crawlerProcessFacilityDefinitions()...)
}

func crawlerAllowPrivateNetworksDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerAllowPrivateNetworks,
		title:       "Allow private-network crawling",
		description: "Allow RFC1918 and IPv6 ULA crawl targets. Loopback, link-local, cloud metadata, carrier-grade NAT, multicast, and reserved ranges remain blocked. Connected crawlers restart automatically.",
		options:     boolSettingOptions(),
		defaultValue: func(config nodeConfig) string {
			return formatSettingBool(config.Crawl.RuntimePolicy.AllowPrivateNetworks)
		},
		normalize: normalizeSettingBool,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.Crawl.RuntimePolicy.AllowPrivateNetworks = value == settingBoolTrue

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			updateCrawlerRuntimePolicy(
				toggles,
				func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
					policy.AllowPrivateNetworks = value == settingBoolTrue
				},
			)
		},
	}
}

func crawlerAllowedPrivateCIDRsDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerAllowCIDRs,
		title:       "Private crawl CIDRs",
		description: "Comma-separated RFC1918 or IPv6 ULA ranges allowed when all private networks are disabled. Reserved and local-only ranges cannot be added. Connected crawlers restart automatically.",
		defaultValue: func(config nodeConfig) string {
			return yagocrawlcontract.FormatCrawlerPrivateCIDRs(
				config.Crawl.RuntimePolicy.AllowedPrivateCIDRs,
			)
		},
		normalize: normalizeCrawlerPrivateCIDRs,
		apply: func(config nodeConfig, value string) nodeConfig {
			prefixes, _ := yagocrawlcontract.ParseCrawlerPrivateCIDRs(value)
			config.Crawl.RuntimePolicy.AllowedPrivateCIDRs = prefixes

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			prefixes, _ := yagocrawlcontract.ParseCrawlerPrivateCIDRs(value)
			updateCrawlerRuntimePolicy(
				toggles,
				func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
					policy.AllowedPrivateCIDRs = prefixes
				},
			)
		},
	}
}

func crawlerBrowserSandboxDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerBrowserSandbox,
		title:       "Firefox content sandbox",
		description: "Keep Firefox content processes inside Firefox's operating-system sandbox. Enable this on hosts that support the required unprivileged namespaces. A live change lets an active render finish, then retires each pooled browser before its next render.",
		options:     boolSettingOptions(),
		defaultValue: func(config nodeConfig) string {
			return formatSettingBool(config.Crawl.RuntimePolicy.BrowserSandbox)
		},
		normalize: normalizeSettingBool,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.Crawl.RuntimePolicy.BrowserSandbox = value == settingBoolTrue

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			updateCrawlerRuntimePolicy(
				toggles,
				func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
					policy.BrowserSandbox = value == settingBoolTrue
				},
			)
		},
	}
}

func crawlerBrowserFailureThresholdDefinition() settingDefinition {
	return crawlerRuntimeIntegerDefinition(crawlerRuntimeIntegerSetting{
		key:         settingKeyCrawlerBrowserFailureLimit,
		title:       "Browser failure threshold",
		description: "Consecutive browser launch or render/navigation failures before the slow-path circuit opens. Zero disables the circuit breaker, so every browser-eligible fetch may attempt the browser path. Connected crawlers restart automatically.",
		minimum:     0,
		maximum:     yagocrawlcontract.MaximumCrawlerBrowserFailureThreshold,
		read: func(policy yagocrawlcontract.CrawlerRuntimePolicy) int {
			return policy.BrowserFailureThreshold
		},
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value int) {
			policy.BrowserFailureThreshold = value
		},
	})
}

func crawlerMaximumDepthDefinition() settingDefinition {
	return crawlerRuntimeIntegerDefinition(crawlerRuntimeIntegerSetting{
		key:         settingKeyCrawlerMaximumDepth,
		title:       "Maximum crawler depth",
		description: "Hard execution ceiling for link depth in every crawl profile. Connected crawlers restart automatically.",
		minimum:     1,
		maximum:     yagocrawlcontract.MaximumCrawlerMaximumDepth,
		read:        func(policy yagocrawlcontract.CrawlerRuntimePolicy) int { return policy.MaximumDepth },
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value int) {
			policy.MaximumDepth = value
		},
	})
}

func crawlerMaximumHostConcurrencyDefinition() settingDefinition {
	return crawlerRuntimeIntegerDefinition(crawlerRuntimeIntegerSetting{
		key:         settingKeyCrawlerMaximumHostFetches,
		title:       "Maximum concurrent fetches per host",
		description: "Maximum in-flight page fetches to one host in each crawler process. Crawl delay remains an additional politeness limit. Connected crawlers restart automatically.",
		minimum:     1,
		maximum:     yagocrawlcontract.MaximumCrawlerMaximumHostConcurrency,
		read: func(policy yagocrawlcontract.CrawlerRuntimePolicy) int {
			return policy.MaximumHostConcurrency
		},
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value int) {
			policy.MaximumHostConcurrency = value
		},
	})
}

func crawlerRunPagesPerMinuteDefinition() settingDefinition {
	return crawlerRuntimeIntegerDefinition(crawlerRuntimeIntegerSetting{
		key:         settingKeyCrawlerRunPagesPerMinute,
		title:       "Default pages per minute per crawl",
		description: "Default fetch-start rate for each crawl run. Zero is unlimited; explicit per-run rates still override it. Connected crawlers restart automatically.",
		minimum:     0,
		maximum:     yagocrawlcontract.MaximumCrawlerRunPagesPerMinute,
		read: func(policy yagocrawlcontract.CrawlerRuntimePolicy) int {
			return int(policy.RunPagesPerMinute)
		},
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value int) {
			policy.RunPagesPerMinute, _ = yagocrawlcontract.ParseCrawlerRunPagesPerMinute(
				strconv.Itoa(value),
			)
		},
	})
}

func crawlerSitemapURLLimitDefinition() settingDefinition {
	return crawlerRuntimeIntegerDefinition(crawlerRuntimeIntegerSetting{
		key:         settingKeyCrawlerSitemapURLLimit,
		title:       "Maximum URLs per sitemap expansion",
		description: "Maximum URL entries admitted from one sitemap, sitemap index, robots sitemap set, or sitelist expansion. Connected crawlers restart automatically.",
		minimum:     1,
		maximum:     yagocrawlcontract.MaximumCrawlerSitemapURLLimit,
		read:        func(policy yagocrawlcontract.CrawlerRuntimePolicy) int { return policy.SitemapURLLimit },
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value int) {
			policy.SitemapURLLimit = value
		},
	})
}

func crawlerConnectTimeoutDefinition() settingDefinition {
	return crawlerRuntimeDurationDefinition(crawlerRuntimeDurationSetting{
		key:         settingKeyCrawlerConnectTimeout,
		title:       "Crawler connect timeout",
		description: "Maximum time to establish an origin TCP connection. Connected crawlers restart automatically.",
		minimum:     yagocrawlcontract.MinimumCrawlerPositiveTimeout,
		maximum:     yagocrawlcontract.MaximumCrawlerPhaseTimeout,
		read: func(policy yagocrawlcontract.CrawlerRuntimePolicy) time.Duration {
			return policy.ConnectTimeout
		},
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value time.Duration) {
			policy.ConnectTimeout = value
		},
	})
}

func crawlerCrawlDelayDefinition() settingDefinition {
	return crawlerRuntimeDurationDefinition(crawlerRuntimeDurationSetting{
		key:         settingKeyCrawlerCrawlDelay,
		title:       "Default crawl delay",
		description: "Minimum delay between fetch starts for the same host when a profile has no larger delay. Zero disables the default delay. Connected crawlers restart automatically.",
		minimum:     0,
		maximum:     yagocrawlcontract.MaximumCrawlerCrawlDelay,
		read:        func(policy yagocrawlcontract.CrawlerRuntimePolicy) time.Duration { return policy.CrawlDelay },
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value time.Duration) {
			policy.CrawlDelay = value
		},
	})
}

func crawlerHeaderTimeoutDefinition() settingDefinition {
	return crawlerRuntimeDurationDefinition(crawlerRuntimeDurationSetting{
		key:         settingKeyCrawlerHeaderTimeout,
		title:       "Crawler response-header timeout",
		description: "Maximum wait for origin response headers after a request is written. Connected crawlers restart automatically.",
		minimum:     yagocrawlcontract.MinimumCrawlerPositiveTimeout,
		maximum:     yagocrawlcontract.MaximumCrawlerPhaseTimeout,
		read:        func(policy yagocrawlcontract.CrawlerRuntimePolicy) time.Duration { return policy.HeaderTimeout },
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value time.Duration) {
			policy.HeaderTimeout = value
		},
	})
}

func crawlerRequestTimeoutDefinition() settingDefinition {
	return crawlerRuntimeDurationDefinition(crawlerRuntimeDurationSetting{
		key:         settingKeyCrawlerRequestTimeout,
		title:       "Crawler request timeout",
		description: "Whole-request deadline covering connection, redirects, headers, and response body. Connected crawlers restart automatically.",
		minimum:     yagocrawlcontract.MinimumCrawlerPositiveTimeout,
		maximum:     yagocrawlcontract.MaximumCrawlerRequestTimeout,
		read:        func(policy yagocrawlcontract.CrawlerRuntimePolicy) time.Duration { return policy.RequestTimeout },
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value time.Duration) {
			policy.RequestTimeout = value
		},
	})
}

func crawlerTLSTimeoutDefinition() settingDefinition {
	return crawlerRuntimeDurationDefinition(crawlerRuntimeDurationSetting{
		key:         settingKeyCrawlerTLSTimeout,
		title:       "Crawler TLS handshake timeout",
		description: "Maximum time for an origin TLS handshake. Connected crawlers restart automatically.",
		minimum:     yagocrawlcontract.MinimumCrawlerPositiveTimeout,
		maximum:     yagocrawlcontract.MaximumCrawlerPhaseTimeout,
		read:        func(policy yagocrawlcontract.CrawlerRuntimePolicy) time.Duration { return policy.TLSTimeout },
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value time.Duration) {
			policy.TLSTimeout = value
		},
	})
}

func crawlerShutdownGraceDefinition() settingDefinition {
	return crawlerRuntimeDurationDefinition(crawlerRuntimeDurationSetting{
		key:         settingKeyCrawlerShutdownGrace,
		title:       "Crawler shutdown grace",
		description: "Maximum drain time for fetch workers and final progress delivery during stop or automatic policy restart. Connected crawlers restart automatically.",
		minimum:     yagocrawlcontract.MinimumCrawlerPositiveTimeout,
		maximum:     yagocrawlcontract.MaximumCrawlerShutdownGrace,
		read:        func(policy yagocrawlcontract.CrawlerRuntimePolicy) time.Duration { return policy.ShutdownGrace },
		write: func(policy *yagocrawlcontract.CrawlerRuntimePolicy, value time.Duration) {
			policy.ShutdownGrace = value
		},
	})
}

func crawlerUserAgentDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyCrawlerUserAgent,
		title:       "Crawler HTTP user agent",
		description: "User-Agent sent for page, robots, sitemap, and browser fetches. Connected crawlers restart automatically.",
		defaultValue: func(config nodeConfig) string {
			return config.Crawl.RuntimePolicy.UserAgent
		},
		normalize: normalizeCrawlerUserAgent,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.Crawl.RuntimePolicy.UserAgent = value

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			updateCrawlerRuntimePolicy(
				toggles,
				func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
					policy.UserAgent = value
				},
			)
		},
	}
}

type crawlerRuntimeIntegerSetting struct {
	key         string
	title       string
	description string
	minimum     int
	maximum     int
	read        func(yagocrawlcontract.CrawlerRuntimePolicy) int
	write       func(*yagocrawlcontract.CrawlerRuntimePolicy, int)
}

func crawlerRuntimeIntegerDefinition(specification crawlerRuntimeIntegerSetting) settingDefinition {
	return settingDefinition{
		key: specification.key, title: specification.title, description: specification.description,
		defaultValue: func(config nodeConfig) string {
			return strconv.Itoa(specification.read(config.Crawl.RuntimePolicy))
		},
		normalize: func(raw string) (string, error) {
			return normalizeCrawlerRuntimeInteger(raw, specification.minimum, specification.maximum)
		},
		apply: func(config nodeConfig, value string) nodeConfig {
			parsed, _ := strconv.Atoi(value)
			specification.write(&config.Crawl.RuntimePolicy, parsed)

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			parsed, _ := strconv.Atoi(value)
			updateCrawlerRuntimePolicy(
				toggles,
				func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
					specification.write(policy, parsed)
				},
			)
		},
	}
}

type crawlerRuntimeDurationSetting struct {
	key         string
	title       string
	description string
	minimum     time.Duration
	maximum     time.Duration
	read        func(yagocrawlcontract.CrawlerRuntimePolicy) time.Duration
	write       func(*yagocrawlcontract.CrawlerRuntimePolicy, time.Duration)
}

func crawlerRuntimeDurationDefinition(
	specification crawlerRuntimeDurationSetting,
) settingDefinition {
	return settingDefinition{
		key: specification.key, title: specification.title, description: specification.description,
		defaultValue: func(config nodeConfig) string {
			return specification.read(config.Crawl.RuntimePolicy).String()
		},
		normalize: func(raw string) (string, error) {
			return normalizeCrawlerRuntimeDuration(
				raw,
				specification.minimum,
				specification.maximum,
			)
		},
		apply: func(config nodeConfig, value string) nodeConfig {
			parsed, _ := time.ParseDuration(value)
			specification.write(&config.Crawl.RuntimePolicy, parsed)

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			parsed, _ := time.ParseDuration(value)
			updateCrawlerRuntimePolicy(
				toggles,
				func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
					specification.write(policy, parsed)
				},
			)
		},
	}
}

func normalizeCrawlerRuntimeInteger(raw string, minimum, maximum int) (string, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minimum || value > maximum {
		return "", fmt.Errorf("value must be an integer between %d and %d", minimum, maximum)
	}

	return strconv.Itoa(value), nil
}

func normalizeCrawlerRuntimeDuration(
	raw string,
	minimum time.Duration,
	maximum time.Duration,
) (string, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value < minimum || value > maximum || value%time.Millisecond != 0 {
		return "", fmt.Errorf(
			"value must be a whole-millisecond duration between %s and %s",
			minimum,
			maximum,
		)
	}

	return value.String(), nil
}

func normalizeCrawlerPrivateCIDRs(raw string) (string, error) {
	prefixes, err := yagocrawlcontract.ParseCrawlerPrivateCIDRs(raw)
	if err != nil {
		return "", fmt.Errorf("normalize crawler private CIDRs: %w", err)
	}

	return yagocrawlcontract.FormatCrawlerPrivateCIDRs(prefixes), nil
}

func normalizeCrawlerUserAgent(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > yagocrawlcontract.MaximumCrawlerUserAgentBytes ||
		strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf(
			"value must be one line between 1 and %d bytes",
			yagocrawlcontract.MaximumCrawlerUserAgentBytes,
		)
	}

	return value, nil
}

func updateCrawlerRuntimePolicy(
	toggles *runtimeToggles,
	update func(*yagocrawlcontract.CrawlerRuntimePolicy),
) {
	if toggles != nil {
		toggles.UpdateCrawlerRuntimePolicy(update)
	}
}
