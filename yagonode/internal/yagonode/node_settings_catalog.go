package yagonode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// extendedSettingDefinitions covers the remaining operator-tunable environment
// settings so the console's Configuration section can override every
// non-secret knob (CFG-02). Secrets — admin credentials and API keys — are
// deliberately absent and stay environment-only. Settings without a live-apply
// hook take effect after a restart, which the console already communicates.
func extendedSettingDefinitions() []settingDefinition {
	definitions := make([]settingDefinition, 0, 12)
	definitions = append(definitions, []settingDefinition{
		{
			key:          "peer.name",
			title:        "Peer name",
			description:  "The name advertised to the swarm (empty keeps the generated name).",
			defaultValue: func(config nodeConfig) string { return config.Name },
			normalize:    parsePeerName,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Name = value

				return config
			},
		},
		{
			key:          "network.advertise.host",
			title:        "Advertised host",
			description:  "Public host peers should dial (empty keeps autodetection).",
			defaultValue: func(config nodeConfig) string { return config.AdvertiseHost },
			normalize:    parseAdvertiseHost,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdvertiseHost = value

				return config
			},
		},
		{
			key:   "network.seedlists",
			title: "Seedlist URLs",
			description: "Comma-separated seedlist URLs imported at startup " +
				"(empty disables seedlist bootstrap imports).",
			defaultValue: func(config nodeConfig) string {
				return strings.Join(config.SeedlistURLs, ",")
			},
			normalize: normalizeSeedlistURLs,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SeedlistURLs, _ = parseSeedlistURLs(value)

				return config
			},
		},
		{
			key:          "search.index.remote",
			title:        "Cache swarm results locally",
			description:  "Index results learned from swarm searches into the local index.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.IndexRemoteResults) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.IndexRemoteResults = value == settingBoolTrue

				return config
			},
		},
	}...)
	definitions = append(definitions, searchSurfaceDefinitions()...)
	definitions = append(definitions, loggingLevelDefinitions()...)
	definitions = append(definitions, extendedTelemetryDefinitions()...)
	definitions = append(definitions, seedCapabilityDefinitions()...)
	definitions = append(definitions, networkDiscoveryDefinitions()...)
	definitions = append(definitions, networkNameDefinitions()...)
	definitions = append(definitions, networkAdvertisementDefinitions()...)
	definitions = append(definitions, remoteSearchDefinitions()...)
	definitions = append(definitions, webFallbackDefinitions()...)
	definitions = append(definitions, extendedGrowthDefinitions()...)
	definitions = append(definitions, storagePressureDefinitions()...)
	definitions = append(definitions, storageReadDeferDefinitions()...)
	definitions = append(definitions, adminOperationsDefinitions()...)

	definitions = append(definitions, autocrawlerDefinitions()...)
	definitions = append(definitions, webDiscoveryDefinitions()...)
	definitions = append(definitions, autocrawlerCrawlOptionDefinitions()...)
	definitions = append(definitions, crawlerRuntimeDefinitions()...)
	definitions = append(definitions, crawlerRuntimePolicyDefinitions()...)

	return append(definitions, parityGapDefinitions()...)
}

// searchSurfaceDefinitions groups the result-link behavior toggles on the public
// search surface: whether links open in a new tab, and whether result clicks are
// captured to mine implicit ranking judgments (YagoRank RANK-00b).
func searchSurfaceDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "search.links.newtab",
			title:        "Open result links in a new tab",
			description:  "Serve result links with target=_blank on the search surfaces.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.SearchLinksNewTab) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SearchLinksNewTab = value == settingBoolTrue

				return config
			},
		},
		{
			key:   "search.click.capture",
			title: "Capture result clicks for ranking",
			description: "Record which result users click to mine implicit " +
				"relevance judgments for the ranking learner (stores query-to-URL " +
				"click aggregates; applied on restart).",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.SearchClickCapture) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SearchClickCapture = value == settingBoolTrue

				return config
			},
		},
	}
}

// parityGapDefinitions groups the settings the CFG-02 review added.
func parityGapDefinitions() []settingDefinition {
	definitions := storageAndAccessDefinitions()
	definitions = append(definitions, swarmPresenceDefinitions()...)
	definitions = append(definitions, dhtDefinitions()...)
	definitions = append(definitions, webFallbackTuningDefinitions()...)
	definitions = append(definitions, searchRateDefinitions()...)

	return append(definitions, perimeterDefinitions()...)
}

// webDiscoveryDefinitions surface the web-fallback seeding path of the
// autocrawler: crawl URLs the web-search fallback surfaced (UI-14).
func webDiscoveryDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "web.fallback.seed_crawl",
			title:        "Web-discovery crawling",
			description:  "Crawl URLs the web-search fallback surfaced so the next identical query is answered locally.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.WebFallback.SeedCrawl) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.SeedCrawl = value == settingBoolTrue

				return config
			},
		},
		{
			key:          "web.fallback.seed_depth",
			title:        "Web-discovery crawl depth",
			description:  "How many link hops each web-surfaced URL is crawled (0 crawls only the URL itself).",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.WebFallback.SeedDepth) },
			normalize:    normalizeSwarmSeedDepth,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.SeedDepth, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:          "web.fallback.seed_max_pages",
			title:        "Web-discovery pages per task",
			description:  "Whole-run page cap for each web-surfaced crawl task. The global crawler run cap may reduce it further.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.WebFallback.SeedMaxPages) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.SeedMaxPages, _ = strconv.Atoi(value)

				return config
			},
		},
	}
}

// autocrawlerDefinitions surface the tunable seed-crawl profile that both the
// swarm greedy-learning and web-fallback discovery paths use (CRAWL-16/UI-14).
func autocrawlerDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "swarm.seed.depth",
			title:        "Autocrawler crawl depth",
			description:  "How many link hops each swarm-surfaced URL is crawled (0 crawls only the URL itself).",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.SwarmSeed.SeedDepth) },
			normalize:    normalizeSwarmSeedDepth,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SwarmSeed.SeedDepth, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:          "swarm.seed.max_pages",
			title:        "Autocrawler pages per task",
			description:  "Whole-run page cap for each swarm-surfaced crawl task. The global crawler run cap may reduce it further.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.SwarmSeed.SeedMaxPages) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SwarmSeed.SeedMaxPages, _ = strconv.Atoi(value)

				return config
			},
		},
	}
}

// autocrawlerCrawlOptionDefinitions surface the per-crawl fetch policy the
// autocrawler applies to its automatic crawls, mirroring the manual crawler's
// options in the crawler's order so both discovery paths obey one policy.
func autocrawlerCrawlOptionDefinitions() []settingDefinition {
	return []settingDefinition{
		boolSettingDefinition(
			"autocrawler.crawl.query_urls",
			"Crawl URLs with query parameters",
			"Crawl URLs that carry query parameters (?...) instead of skipping them.",
			func(config nodeConfig) bool { return config.AutocrawlerCrawl.AllowQueryURLs },
			func(config *nodeConfig, value bool) { config.AutocrawlerCrawl.AllowQueryURLs = value },
		),
		boolSettingDefinition(
			"autocrawler.crawl.tls_insecure",
			"Ignore SSL certificate authority",
			"Accept mis-chained or untrusted TLS certificates when fetching.",
			func(config nodeConfig) bool { return config.AutocrawlerCrawl.IgnoreTLSAuthority },
			func(config *nodeConfig, value bool) { config.AutocrawlerCrawl.IgnoreTLSAuthority = value },
		),
		boolSettingDefinition(
			"autocrawler.crawl.ignore_robots",
			"Ignore robots.txt",
			"Crawl hosts even when robots.txt would disallow it (operator confirms authorisation).",
			func(config nodeConfig) bool { return config.AutocrawlerCrawl.IgnoreRobots },
			func(config *nodeConfig, value bool) { config.AutocrawlerCrawl.IgnoreRobots = value },
		),
		boolSettingDefinition(
			"autocrawler.crawl.no_browser",
			"Disable browser rendering",
			"Fetch pages over plain HTTP only, without headless-browser rendering.",
			func(config nodeConfig) bool { return config.AutocrawlerCrawl.DisableBrowser },
			func(config *nodeConfig, value bool) { config.AutocrawlerCrawl.DisableBrowser = value },
		),
		boolSettingDefinition(
			"autocrawler.crawl.follow_nofollow",
			"Follow rel=nofollow links",
			"Follow links marked rel=nofollow when expanding a crawl.",
			func(config nodeConfig) bool { return config.AutocrawlerCrawl.FollowNoFollowLinks },
			func(config *nodeConfig, value bool) { config.AutocrawlerCrawl.FollowNoFollowLinks = value },
		),
		autocrawlerRecrawlDefinition(),
	}
}

// autocrawlerRecrawlDefinition surfaces the default recrawl cadence the
// autocrawler stamps onto its seeded crawls, so an indexed page is re-fetched
// once it is older than this interval instead of being indexed forever.
func autocrawlerRecrawlDefinition() settingDefinition {
	return settingDefinition{
		key:   "autocrawler.crawl.recrawl_interval",
		title: "Recrawl interval",
		description: "How old an indexed page may get before the autocrawler " +
			"re-fetches it (e.g. 30d, 2w, off). off disables recrawling.",
		defaultValue: func(config nodeConfig) string {
			return yagocrawlcontract.FormatRecrawlInterval(config.AutocrawlerCrawl.RecrawlInterval)
		},
		normalize: normalizeRecrawlInterval,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.AutocrawlerCrawl.RecrawlInterval, _ = yagocrawlcontract.ParseRecrawlInterval(
				value,
			)

			return config
		},
	}
}

// normalizeRecrawlInterval validates a recrawl cadence and returns its
// canonical "30d"/"2w"/"off" form so stored overrides read back consistently
// no matter which accepted spelling the operator submitted.
func normalizeRecrawlInterval(raw string) (string, error) {
	parsed, err := yagocrawlcontract.ParseRecrawlInterval(raw)
	if err != nil {
		return "", fmt.Errorf("autocrawler recrawl interval: %w", err)
	}

	return yagocrawlcontract.FormatRecrawlInterval(parsed), nil
}

// extendedTelemetryDefinitions holds the observability and fallback knobs.
func extendedTelemetryDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "metrics.enabled",
			title:        "Prometheus metrics",
			description:  "Expose /metrics on the ops listener.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.MetricsEnabled) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.MetricsEnabled = value == settingBoolTrue

				return config
			},
		},
		{
			key:         "search.query.log",
			title:       "Query logging",
			description: "How much of each search query reaches the logs.",
			options: []settingOption{
				{value: string(queryLogOff), label: "Off"},
				{value: string(queryLogAggregate), label: "Aggregate"},
				{value: string(queryLogFull), label: "Full"},
			},
			defaultValue: func(config nodeConfig) string { return string(config.QueryLogMode) },
			normalize: func(raw string) (string, error) {
				mode, err := parseQueryLogMode(strings.TrimSpace(raw))
				if err != nil {
					return "", fmt.Errorf("invalid query log mode: %w", err)
				}

				return string(mode), nil
			},
			apply: func(config nodeConfig, value string) nodeConfig {
				config.QueryLogMode = queryLogMode(value)

				return config
			},
		},
	}
}

// networkDiscoveryDefinitions holds the LAN discovery toggle (NET-05).
func networkDiscoveryDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "network.lan_discovery",
			title:        "LAN peer discovery",
			description:  "Announce this node on the local network over a UDP beacon and greet announcing neighbors (Syncthing-style); every discovered peer is verified through the normal hello exchange.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.LANDiscovery) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.LANDiscovery = value == settingBoolTrue

				return config
			},
		},
	}
}

// remoteSearchDefinitions holds the swarm fan-out budgets (SEARCH-35).
func remoteSearchDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "search.remote.peer_timeout",
			title:        "Swarm per-peer timeout",
			description:  "How long one peer may take to contribute to an interactive swarm search.",
			defaultValue: func(config nodeConfig) string { return config.RemotePeerTimeout.String() },
			normalize:    normalizeOutboundRequestTimeout,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemotePeerTimeout, _ = parseOutboundRequestTimeout(value)

				return config
			},
		},
		{
			key:          "search.remote.timeout",
			title:        "Swarm overall timeout",
			description:  "Budget for the whole peer fan-out inside the interactive response deadline.",
			defaultValue: func(config nodeConfig) string { return config.RemoteTimeout.String() },
			normalize:    normalizeOutboundRequestTimeout,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteTimeout, _ = parseOutboundRequestTimeout(value)

				return config
			},
		},
	}
}

// webFallbackDefinitions holds the web-search fallback knobs.
func webFallbackDefinitions() []settingDefinition {
	definitions := make([]settingDefinition, 0, 2)
	definitions = append(definitions, []settingDefinition{
		{
			key:         settingKeyWebFallbackPrivacy,
			title:       "Web search fallback (DDGS)",
			description: "Choose whether DDGS stays disabled, requires request consent, runs after a search miss, or always runs alongside local and swarm retrieval.",
			options: []settingOption{
				{value: string(webFallbackPrivacyDisabled), label: "Disabled"},
				{value: string(webFallbackPrivacyExplicit), label: "Only when requested"},
				{value: string(webFallbackPrivacyEnabled), label: "Enabled on search miss"},
				{value: string(webFallbackPrivacyAlways), label: "Always"},
			},
			defaultValue: func(config nodeConfig) string { return string(config.WebFallback.Privacy) },
			normalize:    normalizeWebFallbackPrivacy,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.Privacy = webFallbackPrivacy(value)

				return config
			},
		},
		{
			key:         "web.fallback.backend",
			title:       "Web fallback engines",
			description: "Which keyless engines answer the web fallback; auto starts DuckDuckGo HTML first, then hedges DuckDuckGo Lite, Brave, Mojeek, and Bing at 50 ms intervals until one answer mentions the query.",
			options: []settingOption{
				{value: "auto", label: "Auto (hedged engines)"},
				{value: "ddg", label: "DuckDuckGo only"},
				{value: "brave", label: "Brave only"},
				{value: "mojeek", label: "Mojeek only"},
				{value: "bing", label: "Bing only"},
			},
			defaultValue: func(config nodeConfig) string { return config.WebFallback.Backend },
			normalize:    normalizeWebFallbackBackend,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.Backend = value

				return config
			},
		},
	}...)

	return definitions
}

// extendedGrowthDefinitions holds the index-growth and fetch knobs.
func extendedGrowthDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "swarm.seed.enabled",
			title:        "Greedy learning",
			description:  "Crawl what swarm search surfaced to grow the local index.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.SwarmSeed.Enabled) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SwarmSeed.Enabled = value == settingBoolTrue

				return config
			},
		},
		{
			key:          "crawl.ingest.quality_gate",
			title:        "Ingest quality gate",
			description:  "Reject crawled pages failing the deterministic Gopher/C4 content-quality rules before they are stored or indexed.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.Crawl.QualityGate) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.QualityGate = value == settingBoolTrue

				return config
			},
		},
		{
			key:          "search.peer.snippet_fetch",
			title:        "Peer snippet fetch",
			description:  "Load the first page's peer results to build verified, query-biased snippets from the pages themselves (YaCy verify parity).",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.PeerSnippetFetch) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.PeerSnippetFetch = value == settingBoolTrue

				return config
			},
		},
		{
			key:          "swarm.morphology.enabled",
			title:        "Swarm morphology",
			description:  "Search peers for bounded inflected forms within each query requirement (more recall, more peer round-trips).",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.SwarmMorphology) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SwarmMorphology = value == settingBoolTrue

				return config
			},
		},
		{
			key:          "extract.fetch.enabled",
			title:        "Fetch on extract",
			description:  "Let /extract, /crawl, and /map fetch pages missing from the index.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.ExtractFetch.Enabled) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.ExtractFetch.Enabled = value == settingBoolTrue

				return config
			},
		},
	}
}

// normalizePositiveInt validates a positive integer setting.
func normalizePositiveInt(raw string) (string, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return "", fmt.Errorf("value must be a positive integer")
	}

	return strconv.Itoa(value), nil
}

// normalizeSwarmSeedDepth accepts a crawl depth within the loader's bounds so a
// runtime edit cannot set an autocrawler depth the env loader would reject.
func normalizeSwarmSeedDepth(raw string) (string, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 || value > maxSwarmSeedDepth {
		return "", fmt.Errorf("value must be an integer between 0 and %d", maxSwarmSeedDepth)
	}

	return strconv.Itoa(value), nil
}
