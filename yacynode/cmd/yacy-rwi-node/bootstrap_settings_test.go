package main

import (
	"testing"
	"time"
)

func TestLoadBootstrapSettingsParsesValues(t *testing.T) {
	settings, err := loadBootstrapSettings(envFrom(map[string]string{
		envSeedlistURLs:     " http://a , http://b ,",
		envAnnounceInterval: "30s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := settings.SeedlistURLs; len(got) != 2 || got[0] != "http://a" ||
		got[1] != "http://b" {
		t.Fatalf("urls = %v, want trimmed pair", got)
	}
	if settings.AnnounceInterval != 30*time.Second {
		t.Fatalf("interval = %v, want 30s", settings.AnnounceInterval)
	}
}

func TestLoadBootstrapSettingsDefaultsInterval(t *testing.T) {
	settings, err := loadBootstrapSettings(envFrom(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings.AnnounceInterval != defaultAnnounceInterval {
		t.Fatalf("interval = %v, want default", settings.AnnounceInterval)
	}
	if got := settings.SeedlistURLs; got != nil {
		t.Fatalf("urls = %v, want nil", got)
	}
}

func TestLoadBootstrapSettingsRejectsBadInterval(t *testing.T) {
	if _, err := loadBootstrapSettings(envFrom(map[string]string{
		envAnnounceInterval: "nope",
	})); err == nil {
		t.Fatal("expected error for unparseable interval")
	}
	if _, err := loadBootstrapSettings(envFrom(map[string]string{
		envAnnounceInterval: "-1s",
	})); err == nil {
		t.Fatal("expected error for non-positive interval")
	}
}
