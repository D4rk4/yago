package adminui

import (
	"net/url"
	"strings"
	"testing"
)

func TestConsoleConfigRendersSensitiveSettingWithoutValue(t *testing.T) {
	view := SettingsView{Items: []SettingItem{{
		Key: "network.authentication.secret", Title: "Shared network secret",
		Value: "must-not-render", Category: "Network & peers",
		Sensitive: true, Configured: true,
	}}}
	response := do(t, New(Options{
		Config: fakeConfig{view: ConfigView{}}, Settings: &fakeSettings{view: view},
	}), "/admin/configuration")
	for _, want := range []string{
		`type="password"`, `autocomplete="new-password"`,
		`placeholder="Configured; leave blank to keep"`,
	} {
		if !strings.Contains(response.body, want) {
			t.Fatalf("sensitive input missing %q", want)
		}
	}
	if strings.Contains(response.body, "must-not-render") {
		t.Fatal("sensitive value rendered in HTML")
	}
}

func TestConsoleConfigLeavesBlankSensitiveSettingUnchanged(t *testing.T) {
	view := SettingsView{Items: []SettingItem{{
		Key: "network.authentication.secret", Title: "Shared network secret",
		Category: "Network & peers", Sensitive: true, Configured: true,
	}}}
	settings := &fakeSettings{view: view, result: SettingsResult{OK: true}}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})
	doPost(t, console, "/admin/configuration", url.Values{
		"key":                                 {"network.authentication.secret"},
		"value:network.authentication.secret": {""},
	})
	if settings.calls != 0 {
		t.Fatalf("blank secret updates = %d", settings.calls)
	}
	doPost(t, console, "/admin/configuration", url.Values{
		"key":                                 {"network.authentication.secret"},
		"value:network.authentication.secret": {"replacement"},
	})
	if settings.calls != 1 || settings.change.Value != "replacement" {
		t.Fatalf("replacement change = %+v, calls=%d", settings.change, settings.calls)
	}
}
