package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

type webFallbackAdminCancellationContext struct {
	context.Context
	calls  int
	failAt int
}

func (c *webFallbackAdminCancellationContext) Err() error {
	c.calls++
	if c.calls >= c.failAt {
		return context.Canceled
	}

	return nil
}

func TestWebFallbackAdminSettingOwnsAlwaysMode(t *testing.T) {
	env := nodeConfig{WebFallback: webFallbackConfig{
		Privacy: webFallbackPrivacyEnabled,
		Trigger: webFallbackTriggerMiss,
	}}
	source, store, _ := newTestSettingsSource(t, env)
	if err := store.Set(
		t.Context(),
		settingKeyLegacyWebFallbackTrigger,
		string(webFallbackTriggerParallel),
	); err != nil {
		t.Fatal(err)
	}
	assertWebFallbackAdminCatalog(t, source.Settings(t.Context()))

	updateWebFallbackAdminMode(t, source, webFallbackPrivacyAlways)
	assertStoredWebFallbackSetting(
		t,
		store,
		settingKeyWebFallbackPrivacy,
		encodeWebFallbackSetting(webFallbackPrivacyAlways),
	)
	assertStoredWebFallbackSetting(
		t,
		store,
		settingKeyLegacyWebFallbackTrigger,
		string(webFallbackTriggerParallel),
	)

	updateWebFallbackAdminMode(t, source, webFallbackPrivacyEnabled)
	assertStoredWebFallbackSetting(
		t,
		store,
		settingKeyWebFallbackPrivacy,
		encodeWebFallbackSetting(webFallbackPrivacyEnabled),
	)
	assertEffectiveStoredWebFallbackMode(t, store, env, webFallbackPrivacyEnabled)

	resetWebFallbackAdminMode(t, source)
	assertStoredWebFallbackSetting(
		t,
		store,
		settingKeyWebFallbackPrivacy,
		webFallbackSettingEnvironment,
	)
	assertEffectiveStoredWebFallbackMode(t, store, env, webFallbackPrivacyEnabled)
	assertWebFallbackAdminResetView(t, source.Settings(t.Context()))
}

func assertWebFallbackAdminCatalog(t *testing.T, view adminui.SettingsView) {
	t.Helper()
	foundAlways := false
	for _, item := range view.Items {
		if item.Key == settingKeyLegacyWebFallbackTrigger {
			t.Fatal("legacy timing setting remains visible")
		}
		if item.Key != settingKeyWebFallbackPrivacy {
			continue
		}
		for _, option := range item.Options {
			if option.Value == string(webFallbackPrivacyAlways) && option.Label == "Always" {
				foundAlways = true
			}
		}
	}
	if !foundAlways {
		t.Fatal("Always option is missing from Web search fallback (DDGS)")
	}
}

func updateWebFallbackAdminMode(
	t *testing.T,
	source *settingsSource,
	mode webFallbackPrivacy,
) {
	t.Helper()
	if _, err := source.Update(t.Context(), adminui.SettingsChange{
		Key: settingKeyWebFallbackPrivacy, Value: string(mode),
	}); err != nil {
		t.Fatal(err)
	}
}

func resetWebFallbackAdminMode(t *testing.T, source *settingsSource) {
	t.Helper()
	if _, err := source.Update(t.Context(), adminui.SettingsChange{
		Key: settingKeyWebFallbackPrivacy, Reset: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func assertWebFallbackAdminResetView(t *testing.T, view adminui.SettingsView) {
	t.Helper()
	for _, item := range view.Items {
		if item.Key == settingKeyWebFallbackPrivacy &&
			(item.Value != string(webFallbackPrivacyEnabled) || item.Overridden) {
			t.Fatalf("reset setting = %#v", item)
		}
	}
}

func assertStoredWebFallbackSetting(
	t *testing.T,
	store *settingsstore.Store,
	key string,
	want string,
) {
	t.Helper()
	value, found, err := store.Get(t.Context(), key)
	if err != nil || !found || value != want {
		t.Fatalf("stored setting = %q/%v/%v, want %q", value, found, err, want)
	}
}

func assertEffectiveStoredWebFallbackMode(
	t *testing.T,
	store *settingsstore.Store,
	env nodeConfig,
	want webFallbackPrivacy,
) {
	t.Helper()
	overrides, err := store.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	config := applyRuntimeSettingOverrides(env, overrides)
	if got := effectiveWebFallbackPrivacy(config.WebFallback); got != want {
		t.Fatalf("effective mode = %q, want %q", got, want)
	}
}

func TestWebFallbackAdminSettingErrorRetainsAuthoritativeValue(t *testing.T) {
	source, store, _ := newTestSettingsSource(t, nodeConfig{})
	stored := encodeWebFallbackSetting(webFallbackPrivacyEnabled)
	if err := store.Set(t.Context(), settingKeyWebFallbackPrivacy, stored); err != nil {
		t.Fatal(err)
	}
	ctx := &webFallbackAdminCancellationContext{
		Context: context.Background(),
		failAt:  2,
	}
	if _, err := source.Update(ctx, adminui.SettingsChange{
		Key: settingKeyWebFallbackPrivacy, Value: string(webFallbackPrivacyAlways),
	}); err == nil {
		t.Fatal("expected setting store error")
	}
	assertStoredWebFallbackSetting(t, store, settingKeyWebFallbackPrivacy, stored)
}

func TestWebFallbackAdminResetErrorRetainsAuthoritativeValue(t *testing.T) {
	source, store, _ := newTestSettingsSource(t, nodeConfig{})
	stored := encodeWebFallbackSetting(webFallbackPrivacyAlways)
	if err := store.Set(t.Context(), settingKeyWebFallbackPrivacy, stored); err != nil {
		t.Fatal(err)
	}
	ctx := &webFallbackAdminCancellationContext{
		Context: context.Background(),
		failAt:  2,
	}
	if _, err := source.Update(ctx, adminui.SettingsChange{
		Key: settingKeyWebFallbackPrivacy, Reset: true,
	}); err == nil {
		t.Fatal("expected setting reset error")
	}
	assertStoredWebFallbackSetting(t, store, settingKeyWebFallbackPrivacy, stored)
}
