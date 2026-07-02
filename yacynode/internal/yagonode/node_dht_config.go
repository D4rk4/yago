package yagonode

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
)

const (
	envNetworkDHT                  = "YACY_NETWORK_DHT"
	envDHTDistribution             = "YACY_DHT_DISTRIBUTION"
	envDHTAllowWhileCrawling       = "YACY_DHT_ALLOW_WHILE_CRAWLING"
	envDHTAllowWhileIndexing       = "YACY_DHT_ALLOW_WHILE_INDEXING"
	envDHTDistributionInterval     = "YACY_DHT_DISTRIBUTION_INTERVAL"
	envDHTRedundancy               = "YACY_DHT_REDUNDANCY"
	envDHTPartitionExponent        = "YACY_DHT_PARTITION_EXPONENT"
	envDHTMinimumPeerAgeDays       = "YACY_DHT_MINIMUM_PEER_AGE_DAYS"
	envDHTMinimumConnectedPeers    = "YACY_DHT_MINIMUM_CONNECTED_PEERS"
	envDHTMinimumRWIWords          = "YACY_DHT_MINIMUM_RWI_WORDS"
	defaultDHTDistributionInterval = 10 * time.Second
	defaultDHTRedundancy           = 3
	defaultDHTPartitionExponent    = 4
	maxDHTRedundancy               = 16
	maxDHTPartitionExponent        = 8
)

type dhtDistributionConfig struct {
	Gates              dhtexchange.GateConfig
	Interval           time.Duration
	Redundancy         int
	PartitionExponent  int
	MinimumPeerAgeDays int
}

func loadDHTDistributionConfig(getenv func(string) string) (dhtDistributionConfig, error) {
	gates, err := loadDHTGateConfig(getenv)
	if err != nil {
		return dhtDistributionConfig{}, err
	}
	interval, err := durationEnv(
		getenv,
		envDHTDistributionInterval,
		defaultDHTDistributionInterval,
	)
	if err != nil {
		return dhtDistributionConfig{}, err
	}
	redundancy, err := intRangeEnv(
		getenv,
		envDHTRedundancy,
		defaultDHTRedundancy,
		1,
		maxDHTRedundancy,
	)
	if err != nil {
		return dhtDistributionConfig{}, err
	}
	partitionExponent, err := intRangeEnv(
		getenv,
		envDHTPartitionExponent,
		defaultDHTPartitionExponent,
		0,
		maxDHTPartitionExponent,
	)
	if err != nil {
		return dhtDistributionConfig{}, err
	}
	minimumPeerAgeDays, err := intAtLeastEnv(
		getenv,
		envDHTMinimumPeerAgeDays,
		dhtexchange.DefaultMinimumPeerAgeDay,
		-1,
	)
	if err != nil {
		return dhtDistributionConfig{}, err
	}

	return dhtDistributionConfig{
		Gates:              gates,
		Interval:           interval,
		Redundancy:         redundancy,
		PartitionExponent:  partitionExponent,
		MinimumPeerAgeDays: minimumPeerAgeDays,
	}, nil
}

func loadDHTGateConfig(getenv func(string) string) (dhtexchange.GateConfig, error) {
	gates := dhtexchange.DefaultGateConfig()

	networkDHT, err := boolEnv(getenv, envNetworkDHT, gates.NetworkDHTEnabled)
	if err != nil {
		return dhtexchange.GateConfig{}, err
	}
	distribution, err := boolEnv(getenv, envDHTDistribution, gates.DistributionEnabled)
	if err != nil {
		return dhtexchange.GateConfig{}, err
	}
	crawling, err := boolEnv(getenv, envDHTAllowWhileCrawling, gates.AllowWhileCrawling)
	if err != nil {
		return dhtexchange.GateConfig{}, err
	}
	indexing, err := boolEnv(getenv, envDHTAllowWhileIndexing, gates.AllowWhileIndexing)
	if err != nil {
		return dhtexchange.GateConfig{}, err
	}
	minimumConnectedPeers, err := intAtLeastEnv(
		getenv,
		envDHTMinimumConnectedPeers,
		dhtexchange.DefaultMinimumConnectedPeers,
		1,
	)
	if err != nil {
		return dhtexchange.GateConfig{}, err
	}
	minimumRWIWords, err := intAtLeastEnv(
		getenv,
		envDHTMinimumRWIWords,
		dhtexchange.DefaultMinimumRWIWords,
		1,
	)
	if err != nil {
		return dhtexchange.GateConfig{}, err
	}

	gates.NetworkDHTEnabled = networkDHT
	gates.DistributionEnabled = distribution
	gates.AllowWhileCrawling = crawling
	gates.AllowWhileIndexing = indexing
	gates.MinimumConnectedPeer = minimumConnectedPeers
	gates.MinimumRWIWord = minimumRWIWords

	return gates, nil
}

func boolEnv(getenv func(string) string, key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}

	return value, nil
}

func durationEnv(
	getenv func(string) string,
	key string,
	fallback time.Duration,
) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}

	return value, nil
}

func intRangeEnv(
	getenv func(string) string,
	key string,
	fallback int,
	minimum int,
	maximum int,
) (int, error) {
	value, ok, err := intEnv(getenv, key, fallback)
	if err != nil {
		return 0, err
	}
	if !ok {
		return value, nil
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("%s: must be between %d and %d", key, minimum, maximum)
	}

	return value, nil
}

func intAtLeastEnv(
	getenv func(string) string,
	key string,
	fallback int,
	minimum int,
) (int, error) {
	value, ok, err := intEnv(getenv, key, fallback)
	if err != nil {
		return 0, err
	}
	if !ok || value >= minimum {
		return value, nil
	}

	return 0, fmt.Errorf("%s: must be at least %d", key, minimum)
}

func intEnv(getenv func(string) string, key string, fallback int) (int, bool, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, false, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, true, fmt.Errorf("%s: %w", key, err)
	}

	return value, true, nil
}
