package adminui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeSettings struct {
	view   SettingsView
	result SettingsResult
	err    error
	calls  int
	change SettingsChange
}

func (f *fakeSettings) Settings(context.Context) SettingsView { return f.view }

func (f *fakeSettings) Update(_ context.Context, change SettingsChange) (SettingsResult, error) {
	f.calls++
	f.change = change

	return f.result, f.err
}

func portalSettingsView(overridden bool) SettingsView {
	return SettingsView{Items: []SettingItem{{
		Key:             "portal.enabled",
		Title:           "Public search portal",
		Value:           "true",
		Overridden:      overridden,
		RestartRequired: true,
		Options: []SettingOption{
			{Value: "true", Label: "Enabled"},
			{Value: "false", Label: "Disabled"},
		},
	}}}
}

func TestConsoleConfigRendersEditableSettings(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Config:   fakeConfig{view: ConfigView{}},
		Settings: &fakeSettings{view: portalSettingsView(true)},
	})
	got := do(t, console, "/admin/configuration")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Runtime settings", `name="key"`, "Public search portal",
		`value="false"`, "Reset to default", `name="csrf_token"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("editable settings missing %q", want)
		}
	}
}

func TestConsoleConfigOmitsEditableSurfaceWithoutSettings(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Config: fakeConfig{view: ConfigView{}}}), "/admin/configuration")
	if strings.Contains(got.body, "Runtime settings") {
		t.Fatal("editable surface rendered without a settings source")
	}
}

func TestConsoleConfigUpdateAppliesChange(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		view: portalSettingsView(true),
		result: SettingsResult{
			OK:              true,
			Message:         "Public search portal updated.",
			RestartRequired: true,
		},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"key":   {"portal.enabled"},
		"value": {"false"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if settings.calls != 1 {
		t.Fatalf("Update called %d times, want 1", settings.calls)
	}
	if settings.change.Key != "portal.enabled" || settings.change.Value != "false" ||
		settings.change.Reset {
		t.Fatalf("unexpected change %+v", settings.change)
	}
	for _, want := range []string{"Public search portal updated.", "Restart the node"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("update response missing %q", want)
		}
	}
}

func TestConsoleConfigUpdateResetClearsOverride(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		view: portalSettingsView(false),
		result: SettingsResult{
			OK:      true,
			Message: "Public search portal reset to the environment default.",
		},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	doPost(t, console, "/admin/configuration", url.Values{
		"key":   {"portal.enabled"},
		"reset": {"true"},
	})
	if !settings.change.Reset {
		t.Fatalf("reset flag not parsed: %+v", settings.change)
	}
}

func TestConsoleConfigUpdateRejectedShowsReason(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		view:   portalSettingsView(false),
		result: SettingsResult{OK: false, Message: "Invalid value for Public search portal."},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"key":   {"portal.enabled"},
		"value": {"maybe"},
	})
	if !strings.Contains(got.body, "Invalid value for Public search portal.") {
		t.Fatal("rejection reason not shown")
	}
}

func TestConsoleConfigUpdateWithoutSettingsNotFound(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}})
	got := doPost(t, console, "/admin/configuration", url.Values{"key": {"portal.enabled"}})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", got.status)
	}
}
