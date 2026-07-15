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
	return settingViewItem(t, view, settingKeyPublicSearchPortal)
}

func settingViewItem(
	t *testing.T,
	view adminui.SettingsView,
	key string,
) adminui.SettingItem {
	t.Helper()

	for _, item := range view.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("setting %q not present in view", key)

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

func TestSettingItemMarksBooleanSetting(t *testing.T) {
	t.Parallel()

	source, _, _ := newTestSettingsSource(t, nodeConfig{PublicSearchUIEnabled: true})
	if item := portalItem(t, source.Settings(context.Background())); !item.Boolean {
		t.Fatal("Enabled/Disabled portal setting should be marked Boolean for checkbox rendering")
	}
}

func TestIsBooleanSettingOptions(t *testing.T) {
	t.Parallel()

	if !isBooleanSettingOptions(boolSettingOptions()) {
		t.Error("Enabled/Disabled options should be detected as boolean")
	}
	for _, options := range [][]settingOption{
		nil,
		{{value: settingBoolTrue, label: "Enabled"}},
		{{value: "a", label: "A"}, {value: "b", label: "B"}},
		{{value: settingBoolTrue, label: "On"}, {value: "off", label: "Off"}},
	} {
		if isBooleanSettingOptions(options) {
			t.Errorf("non-boolean options %+v wrongly detected as boolean", options)
		}
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

func TestSettingsSourceDistinguishesAppliedAndPendingRestartOverrides(t *testing.T) {
	environment := nodeConfig{Name: "environment-name"}
	source, store, _ := newTestSettingsSource(t, environment)
	startup := environment
	startup.Name = "applied-name"
	source.withStartupConfig(startup)
	definition := indexSettingDefinitions()["peer.name"]

	if err := store.Set(context.Background(), definition.key, "applied-name"); err != nil {
		t.Fatalf("Set applied override: %v", err)
	}
	if item := settingViewItem(
		t,
		source.Settings(context.Background()),
		definition.key,
	); item.PendingRestart {
		t.Fatalf("already applied override marked pending: %+v", item)
	}

	if err := store.Set(context.Background(), definition.key, "next-name"); err != nil {
		t.Fatalf("Set pending override: %v", err)
	}
	if item := settingViewItem(
		t,
		source.Settings(context.Background()),
		definition.key,
	); !item.PendingRestart {
		t.Fatalf("unapplied override not marked pending: %+v", item)
	}
	if err := store.Unset(context.Background(), definition.key); err != nil {
		t.Fatalf("Unset pending override: %v", err)
	}
	if item := settingViewItem(
		t,
		source.Settings(context.Background()),
		definition.key,
	); !item.PendingRestart || item.Overridden {
		t.Fatalf("unapplied reset not marked pending: %+v", item)
	}
}

func TestSettingsSourceReportsWebFallbackEnvironmentSentinel(t *testing.T) {
	environment := nodeConfig{WebFallback: webFallbackConfig{Privacy: webFallbackPrivacyAlways}}
	source, store, _ := newTestSettingsSource(t, environment)
	if err := store.Set(
		context.Background(),
		settingKeyWebFallbackPrivacy,
		webFallbackSettingEnvironment,
	); err != nil {
		t.Fatal(err)
	}
	definition := indexSettingDefinitions()[settingKeyWebFallbackPrivacy]
	item := settingViewItem(t, source.Settings(context.Background()), definition.key)
	if item.Value != string(webFallbackPrivacyAlways) || item.Overridden {
		t.Fatalf("item = %q/%v, want environment always", item.Value, item.Overridden)
	}
}

func TestSettingsSourceReportsCanonicalWebFallbackOverride(t *testing.T) {
	source, store, _ := newTestSettingsSource(t, nodeConfig{})
	if err := store.Set(
		context.Background(),
		settingKeyWebFallbackPrivacy,
		encodeWebFallbackSetting(webFallbackPrivacyAlways),
	); err != nil {
		t.Fatal(err)
	}
	definition := indexSettingDefinitions()[settingKeyWebFallbackPrivacy]
	item := settingViewItem(t, source.Settings(context.Background()), definition.key)
	if item.Value != string(webFallbackPrivacyAlways) || !item.Overridden {
		t.Fatalf("item = %q/%v, want overridden always", item.Value, item.Overridden)
	}
}

func TestSettingsSourceReportsUnavailableStoredState(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestSettingsSource(t, nodeConfig{PublicSearchUIEnabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := source.Settings(ctx)
	if view.Error != runtimeSettingsUnavailable || len(view.Items) != 0 {
		t.Fatalf("cancelled read = %+v", view)
	}

	if err := store.Set(
		context.Background(),
		settingKeyPublicSearchPortal,
		"not-a-boolean",
	); err != nil {
		t.Fatalf("Set invalid override: %v", err)
	}
	view = source.Settings(context.Background())
	if view.Error != runtimeSettingsUnavailable || len(view.Items) != 0 {
		t.Fatalf("invalid override = %+v", view)
	}
}

func TestLoadRuntimeSettingsDoesNotMarkDerivedPeerNamePending(t *testing.T) {
	t.Parallel()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	config := testConfig(t)
	config.Hash = ""
	config.Name = ""
	sources, _, effective, err := loadRuntimeSettings(
		context.Background(),
		v,
		config,
		nil,
	)
	if err != nil {
		t.Fatalf("loadRuntimeSettings: %v", err)
	}
	if effective.Name == "" {
		t.Fatal("peer identity did not derive a runtime name")
	}
	item := settingViewItem(
		t,
		sources.settings.Settings(context.Background()),
		"peer.name",
	)
	if item.PendingRestart {
		t.Fatalf("identity-derived peer name marked pending: %+v", item)
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
		"peer.advertise.remote_index":  "Network & peers",
		"peer.advertise.ssl":           "Network & peers",
		"network.advertise.host":       "Network & peers",
		"network.lan_discovery":        "Network & peers",
		"search.links.newtab":          "Search",
		"storage.quota":                "Storage",
		"storage.compaction.interval":  "Storage",
		"swarm.morphology.enabled":     "Swarm",
		"crawl.ingest.quality_gate":    "Crawler",
		"autocrawler.crawl.query_urls": "Crawler",
		"extract.fetch.enabled":        "Extraction",
		"metrics.enabled":              "Monitoring",
		"web.fallback.backend":         "Web fallback",
		"web.fallback.privacy":         "Web fallback",
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
