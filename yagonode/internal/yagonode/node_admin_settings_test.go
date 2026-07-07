package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

func newTestSettingsSource(
	t *testing.T,
	envConfig nodeConfig,
) (*settingsSource, *settingsstore.Store, *events.Recorder) {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}
	recorder := events.NewRecorder(events.DefaultCapacity)
	toggles := newRuntimeToggles(envConfig)

	return newSettingsSource(store, envConfig, toggles, recorder), store, recorder
}

func portalItem(t *testing.T, view adminui.SettingsView) adminui.SettingItem {
	t.Helper()

	for _, item := range view.Items {
		if item.Key == settingKeyPublicSearchPortal {
			return item
		}
	}
	t.Fatal("portal setting not present in view")

	return adminui.SettingItem{}
}

func TestSettingsSourceReportsEnvironmentDefault(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestSettingsSource(t, nodeConfig{PublicSearchUIEnabled: true})

	item := portalItem(t, source.Settings(context.Background()))
	if item.Value != "true" {
		t.Fatalf("value = %q, want environment default true", item.Value)
	}
	if item.Overridden {
		t.Fatal("no override stored, item should not be marked overridden")
	}
}

func TestSettingsSourceReportsStoredOverride(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestSettingsSource(t, nodeConfig{PublicSearchUIEnabled: true})
	if err := store.Set(context.Background(), settingKeyPublicSearchPortal, "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	item := portalItem(t, source.Settings(context.Background()))
	if item.Value != "false" || !item.Overridden {
		t.Fatalf("item = (%q, overridden=%v), want (\"false\", true)", item.Value, item.Overridden)
	}
}

func TestSettingsSourceUpdatePersistsAndRecordsEvent(t *testing.T) {
	t.Parallel()

	source, store, recorder := newTestSettingsSource(t, nodeConfig{PublicSearchUIEnabled: true})
	ctx := context.Background()

	result, err := source.Update(
		ctx,
		adminui.SettingsChange{Key: settingKeyPublicSearchPortal, Value: "false"},
	)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !result.OK || result.RestartRequired {
		t.Fatalf("result = %+v, want OK and live (no restart required)", result)
	}

	stored, set, err := store.Get(ctx, settingKeyPublicSearchPortal)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !set || stored != "false" {
		t.Fatalf("stored = (%q, %v), want (\"false\", true)", stored, set)
	}

	recent := recorder.Recent(1)
	if len(recent) != 1 {
		t.Fatalf("recorded %d events, want 1", len(recent))
	}
	if recent[0].Category != events.CategoryConfig || recent[0].Name != "settings.updated" {
		t.Fatalf("event = %+v, want config/settings.updated", recent[0])
	}
}

func TestSettingsSourceUpdateAppliesPortalLive(t *testing.T) {
	t.Parallel()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}

	envConfig := nodeConfig{PublicSearchUIEnabled: true}
	toggles := newRuntimeToggles(envConfig)
	source := newSettingsSource(
		store,
		envConfig,
		toggles,
		events.NewRecorder(events.DefaultCapacity),
	)
	ctx := context.Background()

	if _, err := source.Update(
		ctx,
		adminui.SettingsChange{Key: settingKeyPublicSearchPortal, Value: "false"},
	); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if toggles.PortalEnabled() {
		t.Fatal("portal toggle was not applied live")
	}

	if _, err := source.Update(
		ctx,
		adminui.SettingsChange{Key: settingKeyPublicSearchPortal, Reset: true},
	); err != nil {
		t.Fatalf("Update reset: %v", err)
	}
	if !toggles.PortalEnabled() {
		t.Fatal("reset did not revert the portal toggle to the environment default")
	}
}

func TestSettingsSourceUpdateResetClearsOverride(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestSettingsSource(t, nodeConfig{PublicSearchUIEnabled: true})
	ctx := context.Background()
	if err := store.Set(ctx, settingKeyPublicSearchPortal, "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	result, err := source.Update(
		ctx,
		adminui.SettingsChange{Key: settingKeyPublicSearchPortal, Reset: true},
	)
	if err != nil {
		t.Fatalf("Update reset: %v", err)
	}
	if !result.OK {
		t.Fatalf("reset result = %+v, want OK", result)
	}

	if _, set, _ := store.Get(ctx, settingKeyPublicSearchPortal); set {
		t.Fatal("override still present after reset")
	}
}

func TestSettingsSourceUpdateRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestSettingsSource(t, nodeConfig{})
	ctx := context.Background()

	result, err := source.Update(
		ctx,
		adminui.SettingsChange{Key: settingKeyPublicSearchPortal, Value: "maybe"},
	)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.OK {
		t.Fatal("invalid value accepted")
	}
	if _, set, _ := store.Get(ctx, settingKeyPublicSearchPortal); set {
		t.Fatal("invalid value was stored")
	}
}

func TestSettingsSourceUpdateRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestSettingsSource(t, nodeConfig{})

	result, err := source.Update(
		context.Background(),
		adminui.SettingsChange{Key: "nope", Value: "true"},
	)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.OK {
		t.Fatal("unknown key accepted")
	}
}

func TestSettingCategory(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"peer.name":                    "General",
		"network.advertise.host":       "Network & peers",
		"network.lan_discovery":        "Network & peers",
		"search.links.newtab":          "Search",
		"swarm.morphology.enabled":     "Swarm",
		"crawl.ingest.quality_gate":    "Crawler",
		"autocrawler.crawl.query_urls": "Crawler",
		"extract.fetch.enabled":        "Extraction",
		"metrics.enabled":              "Monitoring",
		"web.fallback.backend":         "Web fallback",
		"web.robots.policy":            "Public portal",
		"portal.enabled":               "Public portal",
		"public.base.url":              "Public portal",
		"https.redirect":               "Public portal",
		"unknown.key":                  "General",
	}
	for key, want := range cases {
		if got := settingCategory(key); got != want {
			t.Fatalf("settingCategory(%q) = %q, want %q", key, got, want)
		}
	}
}
