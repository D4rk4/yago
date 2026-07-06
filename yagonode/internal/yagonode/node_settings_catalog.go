package yagonode

import (
	"fmt"
	"strconv"
	"strings"
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
			normalize:    normalizeSettingLine,
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
			normalize:    normalizeSettingLine,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdvertiseHost = value

				return config
			},
		},
		{
			key:   "network.seedlists",
			title: "Seedlist URLs",
			description: "Comma-separated seedlist URLs imported at startup " +
				"(empty keeps the built-in defaults).",
			defaultValue: func(config nodeConfig) string {
				return strings.Join(config.SeedlistURLs, ",")
			},
			normalize: normalizeSettingLine,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SeedlistURLs = splitList(value)

				return config
			},
		},
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
	definitions = append(definitions, extendedTelemetryDefinitions()...)
	definitions = append(definitions, extendedGrowthDefinitions()...)

	return append(definitions, autocrawlerDefinitions()...)
}

// autocrawlerDefinitions surface the tunable seed-crawl profile that both the
// swarm greedy-learning and web-fallback discovery paths use (CRAWL-16/UI-14).
func autocrawlerDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "swarm.seed.depth",
			title:        "Autocrawler crawl depth",
			description:  "How many link hops each swarm- or web-surfaced URL is crawled (0 crawls only the URL itself).",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.SwarmSeed.SeedDepth) },
			normalize:    normalizeSwarmSeedDepth,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SwarmSeed.SeedDepth, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:          "swarm.seed.max_pages",
			title:        "Autocrawler pages per host",
			description:  "Cap on how many pages the autocrawler fetches per host for each seeded URL.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.SwarmSeed.SeedMaxPages) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SwarmSeed.SeedMaxPages, _ = strconv.Atoi(value)

				return config
			},
		},
	}
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
		{
			key:         "web.fallback.privacy",
			title:       "Web search fallback (DDGS)",
			description: "Fall back to anonymous web search when the index has no answer.",
			options: []settingOption{
				{value: string(webFallbackPrivacyDisabled), label: "Disabled"},
				{value: string(webFallbackPrivacyExplicit), label: "Only when requested"},
				{value: string(webFallbackPrivacyEnabled), label: "Enabled"},
			},
			defaultValue: func(config nodeConfig) string { return string(config.WebFallback.Privacy) },
			normalize: func(raw string) (string, error) {
				switch webFallbackPrivacy(strings.TrimSpace(strings.ToLower(raw))) {
				case webFallbackPrivacyDisabled,
					webFallbackPrivacyExplicit,
					webFallbackPrivacyEnabled:
					return strings.TrimSpace(strings.ToLower(raw)), nil
				default:
					return "", fmt.Errorf("invalid web fallback privacy")
				}
			},
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.Privacy = webFallbackPrivacy(value)

				return config
			},
		},
	}
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
			key:          "swarm.seed.limit",
			title:        "Greedy learning document limit",
			description:  "Stop swarm-seeded crawling once the index holds this many documents.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.SwarmSeed.LimitDocs) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SwarmSeed.LimitDocs, _ = strconv.Atoi(value)

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
			description:  "Search peers for inflected forms of a single-word query (more recall, more peer round-trips).",
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

// normalizeSettingLine trims a free-form single-line value.
func normalizeSettingLine(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "\r\n") {
		return "", fmt.Errorf("value must be a single line")
	}

	return raw, nil
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
