package yagonode

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/extractfetch"
)

func TestExtractFetchEnvironmentAndAdminShareCanonicalBounds(t *testing.T) {
	definitions := indexSettingDefinitions()
	tests := []struct {
		name        string
		environment string
		setting     string
		raw         string
		canonical   string
		read        func(extractFetchConfig) string
	}{
		{
			name: "minimum timeout", environment: envExtractFetchTimeout,
			setting: "extract.fetch.timeout", raw: "100ms",
			canonical: minimumInteractiveSearchTimeout.String(),
			read: func(config extractFetchConfig) string {
				return config.Timeout.String()
			},
		},
		{
			name: "maximum timeout", environment: envExtractFetchTimeout,
			setting: "extract.fetch.timeout", raw: "2m",
			canonical: maximumInteractiveSearchTimeout.String(),
			read: func(config extractFetchConfig) string {
				return config.Timeout.String()
			},
		},
		{
			name: "minimum response", environment: envExtractFetchMaxBytes,
			setting: "extract.fetch.max_bytes", raw: " 1 ", canonical: "1",
			read: func(config extractFetchConfig) string {
				return strconv.FormatInt(config.MaxBytes, 10)
			},
		},
		{
			name: "maximum response", environment: envExtractFetchMaxBytes,
			setting:   "extract.fetch.max_bytes",
			raw:       strconv.FormatInt(extractfetch.MaximumResponseBytes, 10),
			canonical: strconv.FormatInt(extractfetch.MaximumResponseBytes, 10),
			read: func(config extractFetchConfig) string {
				return strconv.FormatInt(config.MaxBytes, 10)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadExtractFetchConfig(environmentValues{
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
			if value := test.read(applied.ExtractFetch); value != test.canonical {
				t.Fatalf("Admin value = %q, want %q", value, test.canonical)
			}
		})
	}
}

func TestExtractFetchEnvironmentAndAdminRejectTheSameOutOfBoundsValues(t *testing.T) {
	definitions := indexSettingDefinitions()
	tests := []struct {
		name        string
		environment string
		setting     string
		raw         string
	}{
		{
			name:        "short timeout",
			environment: envExtractFetchTimeout,
			setting:     "extract.fetch.timeout",
			raw:         "99ms",
		},
		{
			name:        "long timeout",
			environment: envExtractFetchTimeout,
			setting:     "extract.fetch.timeout",
			raw:         (maximumInteractiveSearchTimeout + time.Nanosecond).String(),
		},
		{
			name:        "zero response",
			environment: envExtractFetchMaxBytes,
			setting:     "extract.fetch.max_bytes",
			raw:         "0",
		},
		{
			name:        "response ceiling plus one",
			environment: envExtractFetchMaxBytes,
			setting:     "extract.fetch.max_bytes",
			raw:         strconv.FormatInt(extractfetch.MaximumResponseBytes+1, 10),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadExtractFetchConfig(environmentValues{
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

func TestExtractFetchOversizedStoredOverrideDoesNotReachRuntime(t *testing.T) {
	base := nodeConfig{ExtractFetch: extractFetchConfig{
		Enabled:  true,
		Timeout:  defaultExtractFetchTimeout,
		MaxBytes: defaultExtractFetchMaxBytes,
	}}
	config := applyRuntimeSettingOverrides(base, map[string]string{
		"extract.fetch.max_bytes": strconv.FormatInt(extractfetch.MaximumResponseBytes+1, 10),
	})
	if config.ExtractFetch.MaxBytes != defaultExtractFetchMaxBytes {
		t.Fatalf("runtime response limit = %d", config.ExtractFetch.MaxBytes)
	}
	if buildExtractFetcher(config, nil) == nil {
		t.Fatal("valid environment fallback did not build the runtime fetcher")
	}
}
