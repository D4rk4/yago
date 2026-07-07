package yagonode

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// This file closes the CFG-02 parity gaps the operator review found: every
// behavior-controlling environment variable gains a matching runtime admin
// setting. Deliberately env-only (identity and boot-time facts, not behavior):
// YAGO_PEER_HASH, YAGO_NETWORK_NAME, YAGO_DATA_DIR, YAGO_PEER_BIRTH_DATE,
// YAGO_ADVERTISE_PORT and the listener addresses (the Listen-addresses UI
// covers those), YAGO_PUBLIC_SELF_TEST_URL; secrets (admin credentials, API
// keys) stay out of the catalog by design.

// storageAndAccessDefinitions covers the storage quota and the agent-API
// access requirement.
func storageAndAccessDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         "storage.quota",
			title:       "Storage quota",
			description: "Disk budget for the vault and index (e.g. 50GB); eviction starts when usage crosses it. The 1GB default is only safe for trials.",
			defaultValue: func(config nodeConfig) string {
				return formatByteSize(config.StorageQuotaByte)
			},
			normalize: normalizeSettingByteSize,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.StorageQuotaByte, _ = parseByteSize(value)

				return config
			},
		},
		{
			key:          "search.api.scoped_access",
			title:        "Require scoped API keys",
			description:  "Authorize the agent search API against the scoped key store instead of the single static token.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.SearchRequireAPIKey) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SearchRequireAPIKey = value == settingBoolTrue

				return config
			},
		},
	}
}

// swarmPresenceDefinitions covers how this node announces itself to peers.
func swarmPresenceDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "network.peer.https_preferred",
			title:        "Prefer HTTPS to peers",
			description:  "Try https first for outbound peer-protocol calls, tolerating self-signed certificates.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.PeerHTTPSPreferred) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.PeerHTTPSPreferred = value == settingBoolTrue

				return config
			},
		},
		{
			key:          "network.announce.interval",
			title:        "Announce interval",
			description:  "How often this node greets peers to stay visible (e.g. 10m).",
			defaultValue: func(config nodeConfig) string { return config.AnnounceInterval.String() },
			normalize:    normalizeSettingLongDuration,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AnnounceInterval, _ = time.ParseDuration(value)

				return config
			},
		},
		{
			key:          "network.announce.greets_per_cycle",
			title:        "Greets per announce cycle",
			description:  "How many peers each announce cycle contacts.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.GreetsPerCycle) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.GreetsPerCycle, _ = strconv.Atoi(value)

				return config
			},
		},
	}
}

// dhtDefinitions covers the distributed-index participation knobs.
func dhtDefinitions() []settingDefinition {
	toggles := []struct {
		key, title, description string
		read                    func(config nodeConfig) bool
		write                   func(config *nodeConfig, value bool)
	}{
		{
			"dht.enabled", "DHT network",
			"Participate in the distributed index at all; off makes this a private crawler node.",
			func(c nodeConfig) bool { return c.DHT.Gates.NetworkDHTEnabled },
			func(c *nodeConfig, v bool) { c.DHT.Gates.NetworkDHTEnabled = v },
		},
		{
			"dht.distribution", "DHT index distribution",
			"Push this node's index entries to their DHT owners.",
			func(c nodeConfig) bool { return c.DHT.Gates.DistributionEnabled },
			func(c *nodeConfig, v bool) { c.DHT.Gates.DistributionEnabled = v },
		},
		{
			"dht.allow_while_crawling", "Distribute while crawling",
			"Keep distributing index entries while a crawl is running.",
			func(c nodeConfig) bool { return c.DHT.Gates.AllowWhileCrawling },
			func(c *nodeConfig, v bool) { c.DHT.Gates.AllowWhileCrawling = v },
		},
		{
			"dht.allow_while_indexing", "Distribute while indexing",
			"Keep distributing index entries while ingest is busy.",
			func(c nodeConfig) bool { return c.DHT.Gates.AllowWhileIndexing },
			func(c *nodeConfig, v bool) { c.DHT.Gates.AllowWhileIndexing = v },
		},
	}
	definitions := make([]settingDefinition, 0, len(toggles)+5)
	for _, toggle := range toggles {
		definitions = append(definitions, boolSettingDefinition(
			toggle.key, toggle.title, toggle.description, toggle.read, toggle.write,
		))
	}

	return append(definitions, dhtTuningDefinitions()...)
}

// boolSettingDefinition builds one boolean catalog entry from accessors.
func boolSettingDefinition(
	key, title, description string,
	read func(config nodeConfig) bool,
	write func(config *nodeConfig, value bool),
) settingDefinition {
	return settingDefinition{
		key:          key,
		title:        title,
		description:  description,
		options:      boolSettingOptions(),
		defaultValue: func(config nodeConfig) string { return formatSettingBool(read(config)) },
		normalize:    normalizeSettingBool,
		apply: func(config nodeConfig, value string) nodeConfig {
			write(&config, value == settingBoolTrue)

			return config
		},
	}
}

func dhtTuningDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "dht.interval",
			title:        "DHT distribution interval",
			description:  "Pause between DHT distribution cycles (e.g. 1m).",
			defaultValue: func(config nodeConfig) string { return config.DHT.Interval.String() },
			normalize:    normalizeSettingLongDuration,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.DHT.Interval, _ = time.ParseDuration(value)

				return config
			},
		},
		{
			key:          "dht.redundancy",
			title:        "DHT redundancy",
			description:  "How many peers receive each index entry.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.DHT.Redundancy) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.DHT.Redundancy, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:          "dht.min_peer_age_days",
			title:        "Minimum peer age (days)",
			description:  "Ignore peers younger than this when picking DHT targets.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.DHT.MinimumPeerAgeDays) },
			normalize:    normalizeNonNegativeInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.DHT.MinimumPeerAgeDays, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:          "dht.min_connected_peers",
			title:        "Minimum connected peers",
			description:  "Hold DHT distribution until at least this many peers are reachable.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.DHT.Gates.MinimumConnectedPeer) },
			normalize:    normalizeNonNegativeInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.DHT.Gates.MinimumConnectedPeer, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:          "dht.min_rwi_words",
			title:        "Minimum indexed words",
			description:  "Hold DHT distribution until the local index holds at least this many words.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.DHT.Gates.MinimumRWIWord) },
			normalize:    normalizeNonNegativeInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.DHT.Gates.MinimumRWIWord, _ = strconv.Atoi(value)

				return config
			},
		},
	}
}

// webFallbackTuningDefinitions covers the fallback's operational knobs beyond
// privacy and backend.
func webFallbackTuningDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "web.fallback.max_results",
			title:        "Web fallback max results",
			description:  "How many web results one fallback query may return.",
			defaultValue: func(config nodeConfig) string { return strconv.Itoa(config.WebFallback.MaxResults) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.MaxResults, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:          "web.fallback.timeout",
			title:        "Web fallback timeout",
			description:  "Budget for one web-engine request (e.g. 10s).",
			defaultValue: func(config nodeConfig) string { return config.WebFallback.Timeout.String() },
			normalize:    normalizeSettingDuration,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.Timeout, _ = time.ParseDuration(value)

				return config
			},
		},
		{
			key:          "web.fallback.cache_ttl",
			title:        "Web fallback cache TTL",
			description:  "How long an answered web query is served from cache (e.g. 5m).",
			defaultValue: func(config nodeConfig) string { return config.WebFallback.CacheTTL.String() },
			normalize:    normalizeSettingLongDuration,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.CacheTTL, _ = time.ParseDuration(value)

				return config
			},
		},
		{
			key:         "web.fallback.safesearch",
			title:       "Web fallback safe search",
			description: "Safe-search mode passed to the web engines.",
			options: []settingOption{
				{value: "moderate", label: "Moderate"},
				{value: "off", label: "Off"},
				{value: "strict", label: "Strict"},
			},
			defaultValue: func(config nodeConfig) string { return config.WebFallback.SafeSearch },
			normalize:    normalizeSafeSearch,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.WebFallback.SafeSearch = value

				return config
			},
		},
		{
			key:          "extract.fetch.timeout",
			title:        "Extract fetch timeout",
			description:  "Budget for one live page fetch on the extract API (e.g. 10s).",
			defaultValue: func(config nodeConfig) string { return config.ExtractFetch.Timeout.String() },
			normalize:    normalizeSettingDuration,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.ExtractFetch.Timeout, _ = time.ParseDuration(value)

				return config
			},
		},
		{
			key:          "extract.fetch.max_bytes",
			title:        "Extract fetch size cap",
			description:  "Largest page body the extract API will download, in bytes.",
			defaultValue: func(config nodeConfig) string { return strconv.FormatInt(config.ExtractFetch.MaxBytes, 10) },
			normalize:    normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.ExtractFetch.MaxBytes, _ = strconv.ParseInt(value, 10, 64)

				return config
			},
		},
	}
}

// perimeterDefinitions covers the egress guard, trusted proxies, and CORS —
// behavior knobs, not secrets, so the console may edit them; each edit takes a
// restart because the guards are built at boot.
func perimeterDefinitions() []settingDefinition {
	return []settingDefinition{
		boolSettingDefinition(
			"security.egress.allow_private",
			"Allow egress to private networks",
			"Let outbound fetches reach RFC1918/loopback addresses. Leave off unless this node must crawl an intranet.",
			func(c nodeConfig) bool { return c.EgressAllowLAN },
			func(c *nodeConfig, v bool) { c.EgressAllowLAN = v },
		),
		{
			key:          "security.egress.allow_cidrs",
			title:        "Egress allow-list CIDRs",
			description:  "Comma-separated private CIDRs outbound fetches may reach even with the private-network guard on.",
			defaultValue: func(config nodeConfig) string { return formatPrefixes(config.EgressAllowedCIDRs) },
			normalize:    normalizeCIDRList,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.EgressAllowedCIDRs, _ = parseEgressAllowCIDRs(value)

				return config
			},
		},
		{
			key:          "security.trusted_proxies",
			title:        "Trusted proxies",
			description:  "Comma-separated CIDRs whose X-Forwarded-For headers are believed (your reverse proxy).",
			defaultValue: func(config nodeConfig) string { return formatIPNets(config.TrustedProxies) },
			normalize:    normalizeCIDRList,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.TrustedProxies, _ = parseTrustedProxies(value)

				return config
			},
		},
		{
			key:          "security.cors.admin",
			title:        "Admin CORS origins",
			description:  "Comma-separated origins allowed to call the admin API cross-origin; empty disables CORS.",
			defaultValue: func(config nodeConfig) string { return strings.Join(config.CrossOrigin.AdminOrigins, ",") },
			normalize:    normalizeSettingLine,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.CrossOrigin.AdminOrigins = splitList(value)

				return config
			},
		},
		{
			key:          "security.cors.search",
			title:        "Search CORS origins",
			description:  "Comma-separated origins allowed to call the search API cross-origin; empty disables CORS.",
			defaultValue: func(config nodeConfig) string { return strings.Join(config.CrossOrigin.SearchOrigins, ",") },
			normalize:    normalizeSettingLine,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.CrossOrigin.SearchOrigins = splitList(value)

				return config
			},
		},
	}
}

// normalizeNonNegativeInt accepts zero and positive integers.
func normalizeNonNegativeInt(raw string) (string, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return "", fmt.Errorf("value must be a non-negative integer")
	}

	return strconv.Itoa(value), nil
}

// normalizeSettingLongDuration accepts durations for slow cycles (announce,
// DHT, caches): thirty seconds to a week.
func normalizeSettingLongDuration(raw string) (string, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value < 30*time.Second || value > 7*24*time.Hour {
		return "", fmt.Errorf("value must be a duration between 30s and 168h")
	}

	return value.String(), nil
}

// normalizeSettingByteSize accepts human-readable sizes of at least 100MB —
// smaller quotas evict the vault into uselessness.
func normalizeSettingByteSize(raw string) (string, error) {
	size, err := parseByteSize(strings.TrimSpace(raw))
	if err != nil || size < 100<<20 {
		return "", fmt.Errorf("value must be a size of at least 100MB (e.g. 50GB)")
	}

	return strings.ToUpper(strings.TrimSpace(raw)), nil
}

func normalizeSafeSearch(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "off", "moderate", "strict":
		return value, nil
	default:
		return "", fmt.Errorf("value must be off, moderate, or strict")
	}
}

// normalizeCIDRList validates a comma-separated CIDR list through the same
// parser the boot path uses.
func normalizeCIDRList(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if _, err := parseTrustedProxies(raw); err != nil {
		return "", fmt.Errorf("value must be comma-separated CIDRs")
	}

	return raw, nil
}

// formatByteSize renders a byte count the way operators write quotas.
func formatByteSize(size int64) string {
	units := []struct {
		suffix string
		factor int64
	}{{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}}
	for _, unit := range units {
		if size >= unit.factor && size%unit.factor == 0 {
			return strconv.FormatInt(size/unit.factor, 10) + unit.suffix
		}
	}

	return strconv.FormatInt(size, 10)
}

// formatPrefixes joins CIDR prefixes back into the comma list operators type.
func formatPrefixes(prefixes []netip.Prefix) string {
	parts := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		parts = append(parts, prefix.String())
	}

	return strings.Join(parts, ",")
}

// formatIPNets joins parsed proxy networks back into the comma list.
func formatIPNets(nets []*net.IPNet) string {
	parts := make([]string, 0, len(nets))
	for _, network := range nets {
		if network != nil {
			parts = append(parts, network.String())
		}
	}

	return strings.Join(parts, ",")
}
