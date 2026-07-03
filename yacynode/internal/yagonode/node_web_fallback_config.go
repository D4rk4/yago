package yagonode

import (
	"fmt"
	"time"
)

const (
	envWebFallbackEnabled    = "YAGO_WEB_FALLBACK_ENABLED"
	envWebFallbackProvider   = "YAGO_WEB_FALLBACK_PROVIDER"
	envWebFallbackBackend    = "YAGO_WEB_FALLBACK_BACKEND"
	envWebFallbackMaxResults = "YAGO_WEB_FALLBACK_MAX_RESULTS"
	envWebFallbackTimeout    = "YAGO_WEB_FALLBACK_TIMEOUT"
	envWebFallbackSafeSearch = "YAGO_WEB_FALLBACK_SAFESEARCH"
	envWebFallbackCacheTTL   = "YAGO_WEB_FALLBACK_CACHE_TTL"
	envWebFallbackSeedCrawl  = "YAGO_WEB_FALLBACK_SEED_CRAWL"

	webFallbackProviderDDGS = "ddgs"

	defaultWebFallbackBackend    = "auto"
	defaultWebFallbackMaxResults = 10
	defaultWebFallbackTimeout    = 10 * time.Second
	defaultWebFallbackSafeSearch = "moderate"
	defaultWebFallbackCacheTTL   = 5 * time.Minute
	minWebFallbackResults        = 1
	maxWebFallbackResults        = 20
)

// webFallbackConfig holds the optional DDGS web-search fallback settings. The
// fallback is off unless Enabled, and never sends a query externally until then.
type webFallbackConfig struct {
	Enabled    bool
	Provider   string
	Backend    string
	MaxResults int
	Timeout    time.Duration
	SafeSearch string
	CacheTTL   time.Duration
	SeedCrawl  bool
}

func loadWebFallbackConfig(getenv func(string) string) (webFallbackConfig, error) {
	enabled, err := boolEnv(getenv, envWebFallbackEnabled, false)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackEnabled, err)
	}
	maxResults, err := intRangeEnv(
		getenv, envWebFallbackMaxResults,
		defaultWebFallbackMaxResults, minWebFallbackResults, maxWebFallbackResults,
	)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackMaxResults, err)
	}
	timeout, err := durationEnv(getenv, envWebFallbackTimeout, defaultWebFallbackTimeout)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackTimeout, err)
	}
	cacheTTL, err := durationEnv(getenv, envWebFallbackCacheTTL, defaultWebFallbackCacheTTL)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackCacheTTL, err)
	}
	seedCrawl, err := boolEnv(getenv, envWebFallbackSeedCrawl, false)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackSeedCrawl, err)
	}

	return webFallbackConfig{
		Enabled:    enabled,
		Provider:   envWithDefault(getenv, envWebFallbackProvider, webFallbackProviderDDGS),
		Backend:    envWithDefault(getenv, envWebFallbackBackend, defaultWebFallbackBackend),
		MaxResults: maxResults,
		Timeout:    timeout,
		SafeSearch: envWithDefault(getenv, envWebFallbackSafeSearch, defaultWebFallbackSafeSearch),
		CacheTTL:   cacheTTL,
		SeedCrawl:  seedCrawl,
	}, nil
}
