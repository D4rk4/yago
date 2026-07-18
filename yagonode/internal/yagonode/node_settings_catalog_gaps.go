package yagonode

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/publicratelimit"
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
			description: "Logical live-data budget for the sharded vault (e.g. 50GB); eviction starts when tracked vault data crosses it. This does not cap the full data directory or search index.",
			defaultValue: func(config nodeConfig) string {
				return formatByteSize(config.StorageQuotaByte)
			},
			normalize: normalizeSettingByteSize,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.StorageQuotaByte, _ = parseByteSize(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				quotaBytes, _ := parseByteSize(value)
				toggles.ApplyStorageQuota(quotaBytes)
			},
		},
		storageCompactionDefinition(),
		storageAutosplitDefinition(),
		storageDeferFsyncDefinition(),
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

// storageCompactionDefinition is the live cadence for rewriting the vault's
// on-disk shards so space freed by evictions and re-ingests is returned to the
// OS instead of lingering as high-water file size (ADR-0036 C).
func storageCompactionDefinition() settingDefinition {
	return settingDefinition{
		key:   "storage.compaction.interval",
		title: "Compaction interval",
		description: "How often the storage engine rewrites its on-disk shards " +
			"to return space freed by deletions to the OS (e.g. 1d, 12h, off). " +
			"off disables compaction.",
		defaultValue: func(config nodeConfig) string {
			return yagocrawlcontract.FormatRecrawlInterval(config.StorageCompaction)
		},
		normalize: normalizeStorageCompaction,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.StorageCompaction, _ = yagocrawlcontract.ParseRecrawlInterval(value)

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			interval, _ := yagocrawlcontract.ParseRecrawlInterval(value)
			toggles.SetCompactionInterval(interval)
		},
	}
}

// normalizeStorageCompaction validates a compaction cadence and returns its
// canonical "1d"/"12h"/"off" form so stored overrides read back consistently.
func normalizeStorageCompaction(raw string) (string, error) {
	parsed, err := yagocrawlcontract.ParseRecrawlInterval(raw)
	if err != nil {
		return "", fmt.Errorf("storage compaction interval: %w", err)
	}

	return yagocrawlcontract.FormatRecrawlInterval(parsed), nil
}

// storageAutosplitDefinition is the live switch for automatic shard-pool growth:
// on, the pool splits its pointer shard as data accumulates so no file grows
// oversized (ADR-0037); off freezes the current shard count.
func storageAutosplitDefinition() settingDefinition {
	return settingDefinition{
		key:   "storage.autosplit",
		title: "Automatic shard growth",
		description: "Grow the storage shard pool automatically as data " +
			"accumulates so no shard file grows oversized. Turn off to freeze the " +
			"current shard count.",
		options:      boolSettingOptions(),
		defaultValue: func(config nodeConfig) string { return formatSettingBool(config.StorageAutosplit) },
		normalize:    normalizeSettingBool,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.StorageAutosplit = value == settingBoolTrue

			return config
		},
		applyLive: func(toggles *runtimeToggles, value string) {
			toggles.SetAutosplitEnabled(value == settingBoolTrue)
		},
	}
}

// storageDeferFsyncDefinition is the restart-required durability switch for the
// vault: on, shard commits skip the per-commit disk flush (bbolt NoSync) and a
// background pass flushes them on a cadence, trading a bounded loss window for
// far less write amplification; off (the default) fsyncs every commit, the only
// crash-safe mode on filesystems without atomic same-file overwrite (ADR-0038).
// It has no live-apply hook, so a change takes effect on the next restart — the
// shards are reconfigured once at boot, never under live writers.
func storageDeferFsyncDefinition() settingDefinition {
	return settingDefinition{
		key:   "storage.defer_fsync",
		title: "Defer storage fsync",
		description: "Skip the per-commit disk flush and flush storage shards " +
			"periodically in the background instead. Faster writes, but a crash or " +
			"power loss can lose the last few seconds of indexing. Leave off unless " +
			"the host has reliable power; takes effect after a restart.",
		options:      boolSettingOptions(),
		defaultValue: func(config nodeConfig) string { return formatSettingBool(config.StorageDeferFsync) },
		normalize:    normalizeSettingBool,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.StorageDeferFsync = value == settingBoolTrue

			return config
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

// seedCapabilityDefinitions surfaces the swarm capability flags this node
// advertises in its seed (Configuration → Network & peers). Editing a flag
// re-derives the advertised bitfield; the change reaches the swarm on the next
// restart, when the seed identity is rebuilt. Accept-remote-crawl is
// intentionally absent: remote crawl execution is disabled for SSRF safety, so
// the node never advertises it.
func seedCapabilityDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:          "peer.advertise.direct_connect",
			title:        "Advertise direct connect",
			description:  "Tell the swarm this peer accepts direct inbound connections (YaCy DirectConnect flag).",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.AdvertiseDirectConnect) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdvertiseDirectConnect = value == settingBoolTrue
				config.Flags = configSeedFlags(config)

				return config
			},
		},
		{
			key:          "peer.advertise.remote_index",
			title:        "Advertise accept remote index",
			description:  "Accept DHT index (RWI) transfers from other peers and advertise that to the swarm. Off also refuses inbound transfers (not_granted).",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.AdvertiseRemoteIndex) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdvertiseRemoteIndex = value == settingBoolTrue
				config.Flags = configSeedFlags(config)

				return config
			},
		},
		{
			key:          "peer.advertise.root_node",
			title:        "Advertise root node",
			description:  "Advertise this peer as a swarm root node (YaCy RootNode flag).",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.AdvertiseRootNode) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdvertiseRootNode = value == settingBoolTrue
				config.Flags = configSeedFlags(config)

				return config
			},
		},
		{
			key:          "peer.advertise.ssl",
			title:        "Advertise SSL available",
			description:  "Advertise that this peer's port serves HTTPS so peers try https first. Enable only when the advertised port actually terminates TLS.",
			options:      boolSettingOptions(),
			defaultValue: func(config nodeConfig) string { return formatSettingBool(config.AdvertiseSSLAvailable) },
			normalize:    normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdvertiseSSLAvailable = value == settingBoolTrue
				config.Flags = configSeedFlags(config)

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

	return strconv.FormatInt(size, 10) + "B"
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

// searchRateTiers falls back to the shipped defaults when the operator has
// not tuned the public search rate limits.
func searchRateTiers(configured publicratelimit.Tiers) publicratelimit.Tiers {
	if configured.Per3Seconds > 0 && configured.PerMinute > 0 && configured.Per10Minutes > 0 {
		return configured
	}

	return publicratelimit.DefaultPublicTiers()
}

// searchRateDefinitions exposes the public-search rate limits — YaCy's
// SearchAccessRate_p parity (UI-20). Authenticated callers keep their 10x
// multiplier on top of whatever the operator sets here.
func searchRateDefinitions() []settingDefinition {
	tiers := []struct {
		key, title, description string
		read                    func(t publicratelimit.Tiers) int
		write                   func(t *publicratelimit.Tiers, v int)
	}{
		{
			"search.rate.burst", "Search burst limit",
			"Anonymous searches allowed per 3 seconds per client.",
			func(t publicratelimit.Tiers) int { return t.Per3Seconds },
			func(t *publicratelimit.Tiers, v int) { t.Per3Seconds = v },
		},
		{
			"search.rate.minute", "Search per-minute limit",
			"Anonymous searches allowed per minute per client.",
			func(t publicratelimit.Tiers) int { return t.PerMinute },
			func(t *publicratelimit.Tiers, v int) { t.PerMinute = v },
		},
		{
			"search.rate.ten_minutes", "Search 10-minute limit",
			"Anonymous searches allowed per ten minutes per client.",
			func(t publicratelimit.Tiers) int { return t.Per10Minutes },
			func(t *publicratelimit.Tiers, v int) { t.Per10Minutes = v },
		},
	}
	definitions := make([]settingDefinition, 0, len(tiers))
	for _, tier := range tiers {
		definitions = append(definitions, settingDefinition{
			key:         tier.key,
			title:       tier.title,
			description: tier.description,
			defaultValue: func(config nodeConfig) string {
				return strconv.Itoa(tier.read(searchRateTiers(config.SearchRate)))
			},
			normalize: normalizePositiveInt,
			apply: func(config nodeConfig, value string) nodeConfig {
				parsed, _ := strconv.Atoi(value)
				config.SearchRate = searchRateTiers(config.SearchRate)
				tier.write(&config.SearchRate, parsed)

				return config
			},
		})
	}

	return definitions
}
