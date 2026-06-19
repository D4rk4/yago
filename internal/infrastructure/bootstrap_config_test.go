package infrastructure

import (
	"slices"
	"testing"
	"time"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadBootstrapSettingsDefaults(t *testing.T) {
	settings, err := LoadBootstrapSettings(envFrom(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings.AnnounceInterval() != defaultAnnounceInterval {
		t.Errorf("interval = %v, want default", settings.AnnounceInterval())
	}
	if settings.SeedlistURLs() != nil {
		t.Errorf("expected empty seedlists, got %v", settings.SeedlistURLs())
	}
}

func TestLoadBootstrapSettingsParsesLists(t *testing.T) {
	settings, err := LoadBootstrapSettings(envFrom(map[string]string{
		EnvSeedlistURLs:     " http://a , http://b ,",
		EnvAnnounceInterval: "30s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"http://a", "http://b"}; !slices.Equal(settings.SeedlistURLs(), want) {
		t.Errorf("seedlists = %v, want %v", settings.SeedlistURLs(), want)
	}
	if settings.AnnounceInterval() != 30*time.Second {
		t.Errorf("interval = %v, want 30s", settings.AnnounceInterval())
	}
}

func TestLoadBootstrapSettingsRejectsBadInterval(t *testing.T) {
	for _, raw := range []string{"nope", "0", "-5m"} {
		if _, err := LoadBootstrapSettings(envFrom(map[string]string{
			EnvAnnounceInterval: raw,
		})); err == nil {
			t.Errorf("interval %q: expected error", raw)
		}
	}
}
