package main

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
	defaultDHTDistributionInterval = 10 * time.Second
)

type dhtDistributionConfig struct {
	Gates    dhtexchange.GateConfig
	Interval time.Duration
}

func loadDHTDistributionConfig(getenv func(string) string) (dhtDistributionConfig, error) {
	gates := dhtexchange.DefaultGateConfig()

	networkDHT, err := boolEnv(getenv, envNetworkDHT, gates.NetworkDHTEnabled)
	if err != nil {
		return dhtDistributionConfig{}, err
	}
	distribution, err := boolEnv(getenv, envDHTDistribution, gates.DistributionEnabled)
	if err != nil {
		return dhtDistributionConfig{}, err
	}
	crawling, err := boolEnv(getenv, envDHTAllowWhileCrawling, gates.AllowWhileCrawling)
	if err != nil {
		return dhtDistributionConfig{}, err
	}
	indexing, err := boolEnv(getenv, envDHTAllowWhileIndexing, gates.AllowWhileIndexing)
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

	gates.NetworkDHTEnabled = networkDHT
	gates.DistributionEnabled = distribution
	gates.AllowWhileCrawling = crawling
	gates.AllowWhileIndexing = indexing

	return dhtDistributionConfig{Gates: gates, Interval: interval}, nil
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
