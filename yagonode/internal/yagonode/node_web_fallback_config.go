package yagonode

import (
	"fmt"
	"strings"
	"time"
)

// webFallbackPrivacy governs whether a search query may be sent to the external
// web-search provider. Disabled never sends a query; explicit sends only when the
// request opts in; enabled sends on any local miss.
type webFallbackPrivacy string

const (
	webFallbackPrivacyDisabled webFallbackPrivacy = "disabled"
	webFallbackPrivacyExplicit webFallbackPrivacy = "explicit"
	webFallbackPrivacyEnabled  webFallbackPrivacy = "enabled"
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
	defaultWebFallbackSeedDepth    = 1
	defaultWebFallbackSeedMaxPages = 20
	minWebFallbackResults          = 1
	maxWebFallbackResults          = 20
	maxWebFallbackSeedDepth        = 8
)

// webFallbackConfig holds the optional DDGS web-search fallback settings. The
// fallback is off unless Enabled, and never sends a query externally until then.
type webFallbackConfig struct {
	Enabled      bool
	Privacy      webFallbackPrivacy
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

	switch webFallbackPrivacy(raw) {
	case webFallbackPrivacyDisabled, webFallbackPrivacyExplicit, webFallbackPrivacyEnabled:
		return webFallbackPrivacy(raw), nil
	default:
		return "", fmt.Errorf("%s: unknown mode %q", envWebFallbackPrivacy, raw)
	}
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
	seedDepth, err := intRangeEnv(
		getenv, envWebFallbackSeedDepth,
		defaultWebFallbackSeedDepth, 0, maxWebFallbackSeedDepth,
	)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackSeedDepth, err)
	}
	seedMaxPages, err := intAtLeastEnv(
		getenv, envWebFallbackSeedMaxPage, defaultWebFallbackSeedMaxPages, 1,
	)
	if err != nil {
		return webFallbackConfig{}, fmt.Errorf("%s: %w", envWebFallbackSeedMaxPage, err)
	}

	return webFallbackConfig{
		Enabled:    enabled,
		Privacy:    privacy,
		Provider:   envWithDefault(getenv, envWebFallbackProvider, webFallbackProviderDDGS),
		Backend:    envWithDefault(getenv, envWebFallbackBackend, defaultWebFallbackBackend),
		MaxResults: maxResults,
		Timeout:    timeout,
		SafeSearch: envWithDefault(
			getenv,
			envWebFallbackSafeSearch,
			defaultWebFallbackSafeSearch,
		),
		CacheTTL:     cacheTTL,
		SeedCrawl:    seedCrawl,
		SeedDepth:    seedDepth,
		SeedMaxPages: seedMaxPages,
	}, nil
}
