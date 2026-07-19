package yagonode

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type webFallbackPrivacy string

const (
	webFallbackPrivacyDisabled webFallbackPrivacy = "disabled"
	webFallbackPrivacyExplicit webFallbackPrivacy = "explicit"
	webFallbackPrivacyEnabled  webFallbackPrivacy = "enabled"
	webFallbackPrivacyAlways   webFallbackPrivacy = "always"
)

const (
	envWebFallbackPrivacy = "YAGO_WEB_FALLBACK_PRIVACY"

	envWebFallbackEnabled     = "YAGO_WEB_FALLBACK_ENABLED"
	envWebFallbackProvider    = "YAGO_WEB_FALLBACK_PROVIDER"
	envWebFallbackBackend     = "YAGO_WEB_FALLBACK_BACKEND"
	envWebFallbackMaxResults  = "YAGO_WEB_FALLBACK_MAX_RESULTS"
	envWebFallbackTimeout     = "YAGO_WEB_FALLBACK_TIMEOUT"
	envWebFallbackSafeSearch  = "YAGO_WEB_FALLBACK_SAFESEARCH"
	envWebFallbackCacheTTL    = "YAGO_WEB_FALLBACK_CACHE_TTL"
	envWebFallbackSeedCrawl   = "YAGO_WEB_FALLBACK_SEED_CRAWL"
	envWebFallbackSeedDepth   = "YAGO_WEB_FALLBACK_SEED_DEPTH"
	envWebFallbackSeedMaxPage = "YAGO_WEB_FALLBACK_SEED_MAX_PAGES"

	webFallbackProviderDDGS = "ddgs"

	defaultWebFallbackBackend      = "auto"
	defaultWebFallbackMaxResults   = 10
	defaultWebFallbackTimeout      = 10 * time.Second
	defaultWebFallbackSafeSearch   = "moderate"
	defaultWebFallbackCacheTTL     = 5 * time.Minute
	defaultWebFallbackSeedDepth    = 5
	defaultWebFallbackSeedMaxPages = 250
	minWebFallbackResults          = 1
	maxWebFallbackResults          = 20
	maxWebFallbackSeedDepth        = 8
)

// webFallbackConfig holds the optional DDGS web-search fallback settings. The
// fallback is off unless Enabled, and never sends a query externally until then.
type webFallbackConfig struct {
	Enabled      bool
	Privacy      webFallbackPrivacy
	Trigger      webFallbackTrigger
	Provider     string
	Backend      string
	MaxResults   int
	Timeout      time.Duration
	SafeSearch   string
	CacheTTL     time.Duration
	SeedCrawl    bool
	SeedDepth    int
	SeedMaxPages int
}

type webFallbackSeedConfig struct {
	enabled  bool
	depth    int
	maxPages int
}

// loadWebFallbackPrivacy resolves the web-fallback privacy mode. When the mode is
// unset it falls back to the legacy YAGO_WEB_FALLBACK_ENABLED flag (enabled ->
// "enabled", otherwise "disabled") so existing deployments keep their behaviour.
func loadWebFallbackPrivacy(
	getenv func(string) string,
	legacyEnabled bool,
) (webFallbackPrivacy, error) {
	raw := strings.TrimSpace(getenv(envWebFallbackPrivacy))
	if raw == "" {
		if legacyEnabled {
			return webFallbackPrivacyEnabled, nil
		}

		return webFallbackPrivacyDisabled, nil
	}

	privacy, err := parseWebFallbackPrivacy(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", envWebFallbackPrivacy, err)
	}

	return privacy, nil
}

func loadWebFallbackConfig(getenv func(string) string) (webFallbackConfig, error) {
	enabled, err := boolEnv(getenv, envWebFallbackEnabled, false)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackEnabled, err)
	}
	privacy, err := loadWebFallbackPrivacy(getenv, enabled)
	if err != nil {
		return webFallbackConfig{}, err
	}
	trigger, err := loadWebFallbackTrigger(getenv)
	if err != nil {
		return webFallbackConfig{}, err
	}
	if err := validateLegacyWebFallbackProvider(getenv(envWebFallbackProvider)); err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackProvider, err)
	}
	backend, err := parseWebFallbackBackend(envWithDefault(
		getenv,
		envWebFallbackBackend,
		defaultWebFallbackBackend,
	))
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackBackend, err)
	}
	maxResults, err := parseWebFallbackMaxResults(envWithDefault(
		getenv,
		envWebFallbackMaxResults,
		strconv.Itoa(defaultWebFallbackMaxResults),
	))
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackMaxResults, err)
	}
	timeout, err := parseOutboundRequestTimeout(envWithDefault(
		getenv,
		envWebFallbackTimeout,
		defaultWebFallbackTimeout.String(),
	))
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackTimeout, err)
	}
	safeSearch, err := parseWebFallbackSafeSearch(envWithDefault(
		getenv,
		envWebFallbackSafeSearch,
		defaultWebFallbackSafeSearch,
	))
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackSafeSearch, err)
	}
	cacheTTL, err := parseWebFallbackCacheTTL(envWithDefault(
		getenv,
		envWebFallbackCacheTTL,
		defaultWebFallbackCacheTTL.String(),
	))
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackCacheTTL, err)
	}
	seed, err := loadWebFallbackSeedConfig(getenv)
	if err != nil {
		return webFallbackConfig{}, err
	}

	return webFallbackConfig{
		Enabled:      enabled,
		Privacy:      privacy,
		Trigger:      trigger,
		Provider:     webFallbackProviderDDGS,
		Backend:      backend,
		MaxResults:   maxResults,
		Timeout:      timeout,
		SafeSearch:   safeSearch,
		CacheTTL:     cacheTTL,
		SeedCrawl:    seed.enabled,
		SeedDepth:    seed.depth,
		SeedMaxPages: seed.maxPages,
	}, nil
}

func loadWebFallbackSeedConfig(
	getenv func(string) string,
) (webFallbackSeedConfig, error) {
	enabled, err := boolEnv(getenv, envWebFallbackSeedCrawl, false)
	if err != nil {
		return webFallbackSeedConfig{}, fmt.Errorf("%s: %w", envWebFallbackSeedCrawl, err)
	}
	depth, err := intRangeEnv(
		getenv, envWebFallbackSeedDepth,
		defaultWebFallbackSeedDepth, 0, maxWebFallbackSeedDepth,
	)
	if err != nil {
		return webFallbackSeedConfig{}, fmt.Errorf("%s: %w", envWebFallbackSeedDepth, err)
	}
	maxPages, err := intAtLeastEnv(
		getenv, envWebFallbackSeedMaxPage, defaultWebFallbackSeedMaxPages, 1,
	)
	if err != nil {
		return webFallbackSeedConfig{}, fmt.Errorf("%s: %w", envWebFallbackSeedMaxPage, err)
	}

	return webFallbackSeedConfig{enabled: enabled, depth: depth, maxPages: maxPages}, nil
}
