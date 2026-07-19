package yagonode

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

type webFallbackCanonicalCase struct {
	name        string
	environment string
	setting     string
	raw         string
	canonical   string
	read        func(webFallbackConfig) string
}

func TestWebFallbackEnvironmentAndAdminShareCanonicalValues(t *testing.T) {
	definitions := indexSettingDefinitions()
	tests := []webFallbackCanonicalCase{
		{
			name: "privacy", environment: envWebFallbackPrivacy,
			setting: settingKeyWebFallbackPrivacy, raw: " ALWAYS ",
			canonical: string(webFallbackPrivacyAlways),
			read: func(config webFallbackConfig) string {
				return string(config.Privacy)
			},
		},
		{
			name: "backend alias", environment: envWebFallbackBackend,
			setting: "web.fallback.backend", raw: " DuckDuckGo ", canonical: "ddg",
			read: func(config webFallbackConfig) string {
				return config.Backend
			},
		},
		{
			name: "safe search", environment: envWebFallbackSafeSearch,
			setting: "web.fallback.safesearch", raw: " STRICT ", canonical: "strict",
			read: func(config webFallbackConfig) string {
				return config.SafeSearch
			},
		},
		{
			name: "minimum results", environment: envWebFallbackMaxResults,
			setting: "web.fallback.max_results", raw: " 1 ", canonical: "1",
			read: func(config webFallbackConfig) string {
				return strconv.Itoa(config.MaxResults)
			},
		},
		{
			name: "maximum results", environment: envWebFallbackMaxResults,
			setting: "web.fallback.max_results", raw: "20", canonical: "20",
			read: func(config webFallbackConfig) string {
				return strconv.Itoa(config.MaxResults)
			},
		},
		{
			name: "minimum timeout", environment: envWebFallbackTimeout,
			setting: "web.fallback.timeout", raw: "100ms",
			canonical: minimumInteractiveSearchTimeout.String(),
			read: func(config webFallbackConfig) string {
				return config.Timeout.String()
			},
		},
		{
			name: "maximum timeout", environment: envWebFallbackTimeout,
			setting: "web.fallback.timeout", raw: "2m",
			canonical: maximumInteractiveSearchTimeout.String(),
			read: func(config webFallbackConfig) string {
				return config.Timeout.String()
			},
		},
		{
			name: "minimum cache TTL", environment: envWebFallbackCacheTTL,
			setting: "web.fallback.cache_ttl", raw: "30s",
			canonical: minimumWebFallbackCacheTTL.String(),
			read: func(config webFallbackConfig) string {
				return config.CacheTTL.String()
			},
		},
		{
			name: "maximum cache TTL", environment: envWebFallbackCacheTTL,
			setting: "web.fallback.cache_ttl", raw: "168h",
			canonical: maximumWebFallbackCacheTTL.String(),
			read: func(config webFallbackConfig) string {
				return config.CacheTTL.String()
			},
		},
	}
	requireWebFallbackCanonicalParity(t, definitions, tests)
}

func requireWebFallbackCanonicalParity(
	t *testing.T,
	definitions map[string]settingDefinition,
	tests []webFallbackCanonicalCase,
) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadWebFallbackConfig(environmentValues{
				test.environment: test.raw,
			}.get)
			if err != nil {
				t.Fatal(err)
			}
			if value := test.read(config); value != test.canonical {
				t.Fatalf("environment value = %q, want %q", value, test.canonical)
			}

			definition := definitions[test.setting]
			normalized, err := definition.normalize(test.raw)
			if err != nil || normalized != test.canonical {
				t.Fatalf("Admin normalization = %q, %v", normalized, err)
			}
			applied := definition.apply(nodeConfig{}, normalized)
			if value := test.read(applied.WebFallback); value != test.canonical {
				t.Fatalf("Admin value = %q, want %q", value, test.canonical)
			}
		})
	}
}

func TestWebFallbackEnvironmentAndAdminRejectTheSameInvalidValues(t *testing.T) {
	definitions := indexSettingDefinitions()
	tests := []struct {
		name        string
		environment string
		setting     string
		raw         string
	}{
		{
			name:        "privacy",
			environment: envWebFallbackPrivacy,
			setting:     settingKeyWebFallbackPrivacy,
			raw:         "sometimes",
		},
		{
			name:        "backend",
			environment: envWebFallbackBackend,
			setting:     "web.fallback.backend",
			raw:         "google",
		},
		{
			name:        "safe search",
			environment: envWebFallbackSafeSearch,
			setting:     "web.fallback.safesearch",
			raw:         "unsafe",
		},
		{
			name:        "zero results",
			environment: envWebFallbackMaxResults,
			setting:     "web.fallback.max_results",
			raw:         "0",
		},
		{
			name:        "too many results",
			environment: envWebFallbackMaxResults,
			setting:     "web.fallback.max_results",
			raw:         "21",
		},
		{
			name:        "short timeout",
			environment: envWebFallbackTimeout,
			setting:     "web.fallback.timeout",
			raw:         "99ms",
		},
		{
			name:        "long timeout",
			environment: envWebFallbackTimeout,
			setting:     "web.fallback.timeout",
			raw:         (maximumInteractiveSearchTimeout + time.Nanosecond).String(),
		},
		{
			name:        "short cache TTL",
			environment: envWebFallbackCacheTTL,
			setting:     "web.fallback.cache_ttl",
			raw:         "29s",
		},
		{
			name:        "long cache TTL",
			environment: envWebFallbackCacheTTL,
			setting:     "web.fallback.cache_ttl",
			raw:         (maximumWebFallbackCacheTTL + time.Nanosecond).String(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadWebFallbackConfig(environmentValues{
				test.environment: test.raw,
			}.get)
			if err == nil || !strings.Contains(err.Error(), test.environment) {
				t.Fatalf("environment error = %v", err)
			}
			if _, err := definitions[test.setting].normalize(test.raw); err == nil {
				t.Fatal("Admin accepted invalid value")
			}
		})
	}
}

func TestWebFallbackLegacyProviderIsExactAndInternallyFixed(t *testing.T) {
	if _, found := indexSettingDefinitions()["web.fallback.provider"]; found {
		t.Fatal("legacy provider is exposed as an Admin setting")
	}
	config, err := loadWebFallbackConfig(environmentValues{
		envWebFallbackProvider: webFallbackProviderDDGS,
	}.get)
	if err != nil || config.Provider != webFallbackProviderDDGS {
		t.Fatalf("provider = %q, %v", config.Provider, err)
	}
	for _, raw := range []string{"DDGS", " ddgs", "ddgs ", "duckduckgo"} {
		if _, err := loadWebFallbackConfig(environmentValues{
			envWebFallbackProvider: raw,
		}.get); err == nil || !strings.Contains(err.Error(), envWebFallbackProvider) {
			t.Fatalf("legacy provider %q error = %v", raw, err)
		}
	}
}
