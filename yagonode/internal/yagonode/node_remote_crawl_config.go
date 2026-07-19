package yagonode

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	envRemoteCrawlEnabled             = "YAGO_REMOTE_CRAWL_ENABLED"
	envRemoteCrawlTrustedPeers        = "YAGO_REMOTE_CRAWL_TRUSTED_PEERS"
	envRemoteCrawlAllowedDestinations = "YAGO_REMOTE_CRAWL_ALLOWED_DESTINATIONS"
	envRemoteCrawlRequestsPerMinute   = "YAGO_REMOTE_CRAWL_REQUESTS_PER_MINUTE"
	envRemoteCrawlOutstandingPerPeer  = "YAGO_REMOTE_CRAWL_OUTSTANDING_PER_PEER"
	envRemoteCrawlLeaseTTL            = "YAGO_REMOTE_CRAWL_LEASE_TTL"
	envRemoteCrawlQueueCapacity       = "YAGO_REMOTE_CRAWL_QUEUE_CAPACITY"
)

type remoteCrawlConfig struct {
	Enabled             bool
	TrustedPeers        []yagomodel.Hash
	AllowedDestinations []string
	RequestsPerMinute   int
	OutstandingPerPeer  int
	LeaseTTL            time.Duration
	QueueCapacity       int
}

func loadRemoteCrawlConfig(getenv func(string) string) (remoteCrawlConfig, error) {
	enabled, err := boolEnv(getenv, envRemoteCrawlEnabled, false)
	if err != nil {
		return remoteCrawlConfig{}, fmt.Errorf("%s: %w", envRemoteCrawlEnabled, err)
	}
	trustedPeers, err := parseRemoteCrawlTrustedPeers(getenv(envRemoteCrawlTrustedPeers))
	if err != nil {
		return remoteCrawlConfig{}, fmt.Errorf("%s: %w", envRemoteCrawlTrustedPeers, err)
	}
	allowedDestinations := splitList(getenv(envRemoteCrawlAllowedDestinations))
	if len(allowedDestinations) > 0 {
		allowedDestinations, err = remotecrawl.NormalizeAllowedDestinations(allowedDestinations)
		if err != nil {
			return remoteCrawlConfig{}, fmt.Errorf("%s: %w", envRemoteCrawlAllowedDestinations, err)
		}
	}
	requestsPerMinute, err := intRangeEnv(
		getenv,
		envRemoteCrawlRequestsPerMinute,
		remotecrawl.DefaultRequestsPerMinute,
		1,
		remotecrawl.MaximumRequestsPerMinute,
	)
	if err != nil {
		return remoteCrawlConfig{}, err
	}
	outstandingPerPeer, err := intRangeEnv(
		getenv,
		envRemoteCrawlOutstandingPerPeer,
		remotecrawl.DefaultOutstandingPerPeer,
		1,
		remotecrawl.MaximumOutstandingPerPeer,
	)
	if err != nil {
		return remoteCrawlConfig{}, err
	}
	leaseTTL, err := durationEnv(getenv, envRemoteCrawlLeaseTTL, remotecrawl.DefaultLeaseTTL)
	if err != nil {
		return remoteCrawlConfig{}, err
	}
	if leaseTTL < time.Second || leaseTTL > remotecrawl.MaximumLeaseTTL {
		return remoteCrawlConfig{}, fmt.Errorf(
			"%s: must be between 1s and %s",
			envRemoteCrawlLeaseTTL,
			remotecrawl.MaximumLeaseTTL,
		)
	}
	queueCapacity, err := intRangeEnv(
		getenv,
		envRemoteCrawlQueueCapacity,
		remotecrawl.DefaultQueueCapacity,
		1,
		remotecrawl.MaximumQueueCapacity,
	)
	if err != nil {
		return remoteCrawlConfig{}, err
	}

	return remoteCrawlConfig{
		Enabled: enabled, TrustedPeers: trustedPeers,
		AllowedDestinations: allowedDestinations,
		RequestsPerMinute:   requestsPerMinute,
		OutstandingPerPeer:  outstandingPerPeer,
		LeaseTTL:            leaseTTL, QueueCapacity: queueCapacity,
	}, nil
}

func parseRemoteCrawlTrustedPeers(raw string) ([]yagomodel.Hash, error) {
	items := splitList(raw)
	if len(items) > remotecrawl.MaximumTrustedPeers {
		return nil, fmt.Errorf(
			"trusted peer hashes must not exceed %d",
			remotecrawl.MaximumTrustedPeers,
		)
	}
	peers := make([]yagomodel.Hash, 0, len(items))
	seen := make(map[yagomodel.Hash]struct{}, len(items))
	for _, item := range items {
		peer, err := yagomodel.ParseHash(item)
		if err != nil {
			return nil, fmt.Errorf("parse trusted peer hash: %w", err)
		}
		if _, duplicate := seen[peer]; duplicate {
			continue
		}
		seen[peer] = struct{}{}
		peers = append(peers, peer)
	}

	return peers, nil
}

func validateRemoteCrawlConfig(config nodeConfig) error {
	if !config.RemoteCrawl.Enabled {
		return nil
	}
	if config.NetworkAuthenticationMode != yagoproto.NetworkAuthenticationSaltedMagic ||
		config.NetworkAuthenticationSecret == "" {
		return fmt.Errorf("remote crawl requires salted-magic network authentication")
	}
	if len(config.RemoteCrawl.TrustedPeers) == 0 {
		return fmt.Errorf("remote crawl requires at least one trusted peer hash")
	}
	if len(config.RemoteCrawl.AllowedDestinations) == 0 {
		return fmt.Errorf("remote crawl requires at least one allowed destination")
	}

	return nil
}

func (config remoteCrawlConfig) brokerConfig() remotecrawl.Config {
	return remotecrawl.Config{
		Enabled: config.Enabled, TrustedPeers: config.TrustedPeers,
		AllowedDestinations: config.AllowedDestinations,
		RequestsPerMinute:   config.RequestsPerMinute,
		OutstandingPerPeer:  config.OutstandingPerPeer,
		LeaseTTL:            config.LeaseTTL, QueueCapacity: config.QueueCapacity,
	}
}

func formatRemoteCrawlTrustedPeers(peers []yagomodel.Hash) string {
	values := make([]string, 0, len(peers))
	for _, peer := range peers {
		values = append(values, peer.String())
	}

	return strings.Join(values, ",")
}

func normalizeRemoteCrawlTrustedPeers(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	peers, err := parseRemoteCrawlTrustedPeers(raw)
	if err != nil || len(peers) == 0 {
		return "", fmt.Errorf("enter one or more comma-separated 12-character peer hashes")
	}

	return formatRemoteCrawlTrustedPeers(peers), nil
}

func normalizeRemoteCrawlDestinations(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	destinations, err := remotecrawl.NormalizeAllowedDestinations(splitList(raw))
	if err != nil {
		return "", fmt.Errorf("normalize remote crawl destinations: %w", err)
	}

	return strings.Join(destinations, ","), nil
}

func normalizeRemoteCrawlLeaseTTL(raw string) (string, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value < time.Second || value > remotecrawl.MaximumLeaseTTL {
		return "", fmt.Errorf("lease TTL must be between 1s and %s", remotecrawl.MaximumLeaseTTL)
	}

	return value.String(), nil
}

func normalizeRemoteCrawlInteger(raw string, maximum int) (string, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 || value > maximum {
		return "", fmt.Errorf("value must be between 1 and %d", maximum)
	}

	return strconv.Itoa(value), nil
}
