package yagonode

import (
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
)

const (
	settingKeyRemoteCrawlEnabled             = "swarm.remote_crawl.enabled"
	settingKeyRemoteCrawlTrustedPeers        = "swarm.remote_crawl.trusted_peers"
	settingKeyRemoteCrawlAllowedDestinations = "swarm.remote_crawl.allowed_destinations"
	settingKeyRemoteCrawlRequestsPerMinute   = "swarm.remote_crawl.requests_per_minute"
	settingKeyRemoteCrawlOutstandingPerPeer  = "swarm.remote_crawl.outstanding_per_peer"
	settingKeyRemoteCrawlLeaseTTL            = "swarm.remote_crawl.lease_ttl"
	settingKeyRemoteCrawlQueueCapacity       = "swarm.remote_crawl.queue_capacity"
)

func remoteCrawlSettingDefinitions() []settingDefinition {
	return append(remoteCrawlPeerSettingDefinitions(), remoteCrawlQueueSettingDefinitions()...)
}

func remoteCrawlPeerSettingDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyRemoteCrawlTrustedPeers,
			title:       "Remote crawl trusted peers",
			description: "One to 256 comma-separated YaCy peer hashes allowed to receive crawl leases and submit receipts. Changes take effect after a restart.",
			defaultValue: func(config nodeConfig) string {
				return formatRemoteCrawlTrustedPeers(config.RemoteCrawl.TrustedPeers)
			},
			normalize: normalizeRemoteCrawlTrustedPeers,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteCrawl.TrustedPeers, _ = parseRemoteCrawlTrustedPeers(value)

				return config
			},
		},
		{
			key:         settingKeyRemoteCrawlAllowedDestinations,
			title:       "Remote crawl allowed destinations",
			description: "One to 256 comma-separated exact domains and IP ranges that may be delegated. Address-family wildcards are rejected. Only default HTTP and HTTPS ports are accepted. Changes take effect after a restart.",
			defaultValue: func(config nodeConfig) string {
				return strings.Join(config.RemoteCrawl.AllowedDestinations, ",")
			},
			normalize: normalizeRemoteCrawlDestinations,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteCrawl.AllowedDestinations = splitList(value)

				return config
			},
		},
		{
			key:         settingKeyRemoteCrawlRequestsPerMinute,
			title:       "Remote crawl requests per minute",
			description: "Maximum remote-crawl feed requests accepted from each trusted peer per minute. Changes take effect after a restart.",
			defaultValue: func(config nodeConfig) string {
				return strconv.Itoa(config.RemoteCrawl.RequestsPerMinute)
			},
			normalize: func(raw string) (string, error) {
				return normalizeRemoteCrawlInteger(raw, remotecrawl.MaximumRequestsPerMinute)
			},
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteCrawl.RequestsPerMinute, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:         settingKeyRemoteCrawlOutstandingPerPeer,
			title:       "Remote crawl outstanding leases",
			description: "Maximum crawl leases a trusted peer may hold at once. Changes take effect after a restart.",
			defaultValue: func(config nodeConfig) string {
				return strconv.Itoa(config.RemoteCrawl.OutstandingPerPeer)
			},
			normalize: func(raw string) (string, error) {
				return normalizeRemoteCrawlInteger(raw, remotecrawl.MaximumOutstandingPerPeer)
			},
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteCrawl.OutstandingPerPeer, _ = strconv.Atoi(value)

				return config
			},
		},
	}
}

func remoteCrawlQueueSettingDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyRemoteCrawlLeaseTTL,
			title:       "Remote crawl lease TTL",
			description: "How long delegated work remains assigned before it is returned to the durable queue. Changes take effect after a restart.",
			defaultValue: func(config nodeConfig) string {
				return config.RemoteCrawl.LeaseTTL.String()
			},
			normalize: normalizeRemoteCrawlLeaseTTL,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteCrawl.LeaseTTL, _ = time.ParseDuration(value)

				return config
			},
		},
		{
			key:         settingKeyRemoteCrawlQueueCapacity,
			title:       "Remote crawl queue capacity",
			description: "Maximum distinct locally accepted URLs retained for remote delegation. Local crawler orders remain authoritative when this queue is full. Changes take effect after a restart.",
			defaultValue: func(config nodeConfig) string {
				return strconv.Itoa(config.RemoteCrawl.QueueCapacity)
			},
			normalize: func(raw string) (string, error) {
				return normalizeRemoteCrawlInteger(raw, remotecrawl.MaximumQueueCapacity)
			},
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteCrawl.QueueCapacity, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:         settingKeyRemoteCrawlEnabled,
			title:       "Remote crawl delegation",
			description: "Delegate locally accepted crawl URLs through the YaCy remote-crawl protocol. Requires salted-magic authentication, trusted peers, and allowed destinations. Changes take effect after a restart.",
			options:     boolSettingOptions(),
			defaultValue: func(config nodeConfig) string {
				return formatSettingBool(config.RemoteCrawl.Enabled)
			},
			normalize: normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.RemoteCrawl.Enabled = value == settingBoolTrue
				config.Flags = configSeedFlags(config)

				return config
			},
		},
	}
}
