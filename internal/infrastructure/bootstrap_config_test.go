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
	if settings.SeedlistURLs() != nil || settings.BootstrapPeers() != nil {
		t.Errorf(
			"expected empty lists, got %v %v",
			settings.SeedlistURLs(),
			settings.BootstrapPeers(),
		)
	}
}

func TestLoadBootstrapSettingsParsesLists(t *testing.T) {
	settings, err := LoadBootstrapSettings(envFrom(map[string]string{
		EnvSeedlistURLs:     " http://a , http://b ,",
		EnvBootstrapPeers:   "203.0.113.1:8090,,203.0.113.2:8090",
		EnvAnnounceInterval: "30s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"http://a", "http://b"}; !slices.Equal(settings.SeedlistURLs(), want) {
		t.Errorf("seedlists = %v, want %v", settings.SeedlistURLs(), want)
	}
	if want := []string{"203.0.113.1:8090", "203.0.113.2:8090"}; !slices.Equal(
		settings.BootstrapPeers(), want,
	) {
		t.Errorf("peers = %v, want %v", settings.BootstrapPeers(), want)
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
