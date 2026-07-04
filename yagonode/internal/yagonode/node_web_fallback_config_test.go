package yagonode

import (
	"testing"
	"time"
)

func TestLoadWebFallbackConfigDefaults(t *testing.T) {
	config, err := loadWebFallbackConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if config.Enabled {
		t.Error("web fallback must be disabled by default")
	}
	if config.Provider != "ddgs" || config.Backend != "auto" {
		t.Errorf("provider/backend = %q/%q", config.Provider, config.Backend)
	}
	if config.MaxResults != defaultWebFallbackMaxResults {
		t.Errorf("maxResults = %d", config.MaxResults)
	}
	if config.SeedCrawl {
		t.Error("seed crawl must be off by default")
	}
}

func TestLoadWebFallbackConfigParsesValues(t *testing.T) {
	env := map[string]string{
		envWebFallbackEnabled:    "true",
		envWebFallbackBackend:    "mojeek",
		envWebFallbackMaxResults: "5",
		envWebFallbackTimeout:    "3s",
		envWebFallbackSeedCrawl:  "true",
	}
	config, err := loadWebFallbackConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !config.Enabled || config.Backend != "mojeek" || config.MaxResults != 5 ||
		config.Timeout != 3*time.Second || !config.SeedCrawl {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadWebFallbackConfigRejectsBadBool(t *testing.T) {
	_, err := loadWebFallbackConfig(func(key string) string {
		if key == envWebFallbackEnabled {
			return "notabool"
		}

		return ""
	})
	if err == nil {
		t.Fatal("expected error for an invalid boolean")
	}
}

func TestLoadWebFallbackConfigRejectsOutOfRangeResults(t *testing.T) {
	_, err := loadWebFallbackConfig(func(key string) string {
		if key == envWebFallbackMaxResults {
			return "999"
		}

		return ""
	})
	if err == nil {
		t.Fatal("expected error for out-of-range max results")
	}
}

func TestLoadWebFallbackPrivacyDefaultsToDisabled(t *testing.T) {
	config, err := loadWebFallbackConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if config.Privacy != webFallbackPrivacyDisabled {
		t.Fatalf("privacy = %q, want disabled", config.Privacy)
	}
}

func TestLoadWebFallbackPrivacyFollowsLegacyEnabled(t *testing.T) {
	config, err := loadWebFallbackConfig(func(key string) string {
		if key == envWebFallbackEnabled {
			return "true"
		}

		return ""
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if config.Privacy != webFallbackPrivacyEnabled {
		t.Fatalf("privacy = %q, want enabled from legacy flag", config.Privacy)
	}
}

func TestLoadWebFallbackPrivacyExplicitOverridesLegacy(t *testing.T) {
	env := map[string]string{
		envWebFallbackEnabled: "true",
		envWebFallbackPrivacy: string(webFallbackPrivacyExplicit),
	}
	config, err := loadWebFallbackConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if config.Privacy != webFallbackPrivacyExplicit {
		t.Fatalf("privacy = %q, want explicit", config.Privacy)
	}
}

func TestLoadWebFallbackPrivacyRejectsUnknownMode(t *testing.T) {
	_, err := loadWebFallbackConfig(func(key string) string {
		if key == envWebFallbackPrivacy {
			return "sometimes"
		}

		return ""
	})
	if err == nil {
		t.Fatal("expected error for an unknown privacy mode")
	}
}
