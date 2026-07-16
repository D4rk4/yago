package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeSettings struct {
	view    SettingsView
	result  SettingsResult
	err     error
	calls   int
	change  SettingsChange
	changes []SettingsChange
}

func (f *fakeSettings) Settings(context.Context) SettingsView { return f.view }

func (f *fakeSettings) Update(_ context.Context, change SettingsChange) (SettingsResult, error) {
	f.calls++
	f.change = change
	f.changes = append(f.changes, change)

	return f.result, f.err
}

func portalSettingsView(overridden bool) SettingsView {
	return SettingsView{Items: []SettingItem{{
		Key:             "portal.enabled",
		Title:           "Public search portal",
		Value:           "true",
		Overridden:      overridden,
		RestartRequired: true,
		Category:        "Search",
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
		`role="tablist"`, `id="tab-search"`, `aria-controls="panel-search"`,
		`name="key"`, "Public search portal", `class="cds-setting-row"`,
		`name="value:portal.enabled"`, `value="false"`, `name="csrf_token"`,
		`name="reset" value="portal.enabled"`, `formnovalidate`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("editable settings missing %q", want)
		}
	}
}

func TestConsoleConfigLabelsPendingRestartValues(t *testing.T) {
	t.Parallel()

	view := portalSettingsView(true)
	view.Items[0].PendingRestart = true
	got := do(t, New(Options{
		Config: fakeConfig{view: ConfigView{}}, Settings: &fakeSettings{view: view},
	}), "/admin/configuration")
	for _, want := range []string{"pending restart", "desired settings", "stored but not yet applied"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("pending-restart truth missing %q", want)
		}
	}
}

func TestConsoleConfigRendersAlwaysWebFallbackMode(t *testing.T) {
	t.Parallel()

	view := SettingsView{Items: []SettingItem{{
		Key: "web.fallback.privacy", Title: "Web search fallback (DDGS)", Value: "enabled",
		Category: "Web fallback", RestartRequired: true,
		Options: []SettingOption{
			{Value: "enabled", Label: "Enabled on search miss"},
			{Value: "always", Label: "Always"},
		},
	}}}
	settings := &fakeSettings{view: view, result: SettingsResult{OK: true}}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})
	response := do(t, console, "/admin/configuration")
	for _, want := range []string{
		`id="panel-web-fallback"`, `name="value:web.fallback.privacy"`,
		`value="always"`, "Always",
	} {
		if !strings.Contains(response.body, want) {
			t.Fatalf("web fallback setting missing %q", want)
		}
	}

	doPost(t, console, "/admin/configuration", url.Values{
		"key":                        {"web.fallback.privacy"},
		"value:web.fallback.privacy": {"always"},
	})
	if settings.calls != 1 || settings.change.Key != "web.fallback.privacy" ||
		settings.change.Value != "always" {
		t.Fatalf("change = %#v, calls = %d", settings.change, settings.calls)
	}
}

// TestConsoleConfigTabRendersOneFormWithOneSave is the operator-feedback
// acceptance: each tab is a single form under one shared Save button, with each
// setting collapsed to one aligned row rather than its own stacked form.
func TestConsoleConfigTabRendersOneFormWithOneSave(t *testing.T) {
	t.Parallel()

	view := SettingsView{Items: []SettingItem{
		{
			Key: "portal.enabled", Title: "Public search portal", Value: "true",
			Category: "Search", Boolean: true,
			Options: []SettingOption{
				{Value: "true", Label: "Enabled"},
				{Value: "false", Label: "Disabled"},
			},
		},
		{Key: "portal.title", Title: "Portal title", Value: "Yago", Category: "Search"},
	}}
	console := New(
		Options{Config: fakeConfig{view: ConfigView{}}, Settings: &fakeSettings{view: view}},
	)
	got := do(t, console, "/admin/configuration")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if n := strings.Count(got.body, `action="/admin/configuration#panel-search"`); n != 1 {
		t.Fatalf("settings forms = %d, want 1 (one form per tab)", n)
	}
	if n := strings.Count(got.body, `>Save</button>`); n != 1 {
		t.Fatalf("Save buttons = %d, want 1 (one shared Save)", n)
	}
	if n := strings.Count(got.body, `class="cds-setting-row"`); n != 2 {
		t.Fatalf("one-line rows = %d, want 2", n)
	}
}

func booleanSettingsView() SettingsView {
	return SettingsView{Items: []SettingItem{{
		Key:      "portal.enabled",
		Title:    "Public search portal",
		Value:    "true",
		Category: "Search",
		Boolean:  true,
		Options: []SettingOption{
			{Value: "true", Label: "Enabled"},
			{Value: "false", Label: "Disabled"},
		},
	}}}
}

func TestConsoleConfigRendersBooleanSettingAsCheckbox(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Config:   fakeConfig{view: ConfigView{}},
		Settings: &fakeSettings{view: booleanSettingsView()},
	})
	got := do(t, console, "/admin/configuration")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		`class="cds-fieldset"`, `<legend class="cds-legend">`,
		`type="checkbox"`, `name="bool:portal.enabled"`, `class="cds-checkbox"`,
		`value="true" checked`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("boolean setting render missing %q", want)
		}
	}
	if strings.Contains(got.body, "<select") {
		t.Fatal("a boolean setting must render a checkbox, not a dropdown")
	}
	if strings.Contains(got.body, `name="reset"`) {
		t.Fatal("a setting without an override must not render Reset")
	}
}

func TestConsoleConfigCheckboxCheckedSubmitsTrue(t *testing.T) {
	t.Parallel()

	view := SettingsView{Items: []SettingItem{{
		Key: "portal.enabled", Title: "Public search portal", Value: "false",
		Category: "Search", Boolean: true,
		Options: []SettingOption{
			{Value: "true", Label: "Enabled"},
			{Value: "false", Label: "Disabled"},
		},
	}}}
	settings := &fakeSettings{view: view, result: SettingsResult{OK: true}}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	doPost(t, console, "/admin/configuration", url.Values{
		"key":                  {"portal.enabled"},
		"bool:portal.enabled":  {"1"},
		"value:portal.enabled": {"true"},
	})
	if settings.calls != 1 || settings.change.Value != "true" {
		t.Fatalf(
			"checked checkbox: calls=%d value=%q, want 1 true",
			settings.calls, settings.change.Value,
		)
	}
}

func TestConsoleConfigCheckboxUncheckedSubmitsFalse(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{view: booleanSettingsView(), result: SettingsResult{OK: true}}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	doPost(t, console, "/admin/configuration", url.Values{
		"key":                 {"portal.enabled"},
		"bool:portal.enabled": {"1"},
	})
	if settings.calls != 1 || settings.change.Value != "false" {
		t.Fatalf(
			"unchecked checkbox: calls=%d value=%q, want 1 false (absent value coerced)",
			settings.calls, settings.change.Value,
		)
	}
}

func TestConsoleConfigOmitsEditableSurfaceWithoutSettings(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Config: fakeConfig{view: ConfigView{}}}), "/admin/configuration")
	if strings.Contains(got.body, `for="setting-`) {
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
		"key":                  {"portal.enabled"},
		"value:portal.enabled": {"false"},
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
	for _, want := range []string{"1 setting updated.", "Restart the node"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("update response missing %q", want)
		}
	}
}

// TestConsoleConfigSaveAppliesOnlyChangedSettings is the batch acceptance: a
// single Save writes only the rows whose value differs, so an unchanged setting
// is never re-applied (and never spuriously marked overridden).
func TestConsoleConfigSaveAppliesOnlyChangedSettings(t *testing.T) {
	t.Parallel()

	view := SettingsView{Items: []SettingItem{
		{Key: "a.one", Value: "keep", Category: "General"},
		{Key: "a.two", Value: "old2", Category: "General"},
		{Key: "a.three", Value: "old3", Category: "General"},
	}}
	settings := &fakeSettings{view: view, result: SettingsResult{OK: true}}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"key":           {"a.one", "a.two", "a.three"},
		"value:a.one":   {"keep"}, // unchanged, skipped
		"value:a.two":   {"new2"}, // changed
		"value:a.three": {"new3"}, // changed
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if settings.calls != 2 {
		t.Fatalf("Update called %d times, want 2 (only the changed keys)", settings.calls)
	}
	changed := map[string]string{}
	for _, ch := range settings.changes {
		changed[ch.Key] = ch.Value
	}
	if changed["a.two"] != "new2" || changed["a.three"] != "new3" || len(changed) != 2 {
		t.Fatalf("applied changes = %+v, want only a.two/a.three", settings.changes)
	}
	if !strings.Contains(got.body, "2 settings updated.") {
		t.Fatalf("missing batch summary, got %.80q", got.body)
	}
}

func TestConsoleConfigSaveNoChanges(t *testing.T) {
	t.Parallel()

	view := SettingsView{Items: []SettingItem{{Key: "a.one", Value: "keep", Category: "General"}}}
	settings := &fakeSettings{view: view, result: SettingsResult{OK: true}}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"key":         {"a.one"},
		"value:a.one": {"keep"},
	})
	if settings.calls != 0 {
		t.Fatalf("Update called %d times, want 0 for an unchanged Save", settings.calls)
	}
	if !strings.Contains(got.body, "No changes.") {
		t.Fatalf("missing no-changes notice, got %.80q", got.body)
	}
}

func TestConsoleConfigUpdateResetClearsOverride(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		view: portalSettingsView(true),
		result: SettingsResult{
			OK:      true,
			Message: "Public search portal reset to the environment default.",
		},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	doPost(t, console, "/admin/configuration", url.Values{
		"key":                  {"portal.enabled"},
		"value:portal.enabled": {"false"},
		"reset":                {"portal.enabled"},
	})
	if !settings.change.Reset || settings.change.Key != "portal.enabled" {
		t.Fatalf("reset flag not parsed: %+v", settings.change)
	}
	if settings.calls != 1 {
		t.Fatalf(
			"reset applied %d updates, want exactly 1 (reset takes precedence)",
			settings.calls,
		)
	}
}

func TestConsoleConfigResetErrorShowsGeneric(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{view: portalSettingsView(true), err: errors.New("backend detail")}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	got := doPost(t, console, "/admin/configuration", url.Values{"reset": {"portal.enabled"}})
	if !strings.Contains(got.body, "Update failed. Please try again.") {
		t.Fatalf("reset error not generic: %.80q", got.body)
	}
	if strings.Contains(got.body, "backend detail") {
		t.Fatal("must not leak internal error detail")
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
		"key":                  {"portal.enabled"},
		"value:portal.enabled": {"maybe"},
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

func TestRuntimeSettingsReadFailureRendersUnavailable(t *testing.T) {
	t.Parallel()

	const message = "Stored runtime settings are unavailable."
	settings := &fakeSettings{view: SettingsView{Error: message}}
	pages := []struct {
		path    string
		console *Console
	}{
		{
			path: "/admin/configuration",
			console: New(Options{
				Config: fakeConfig{view: ConfigView{}}, Settings: settings,
			}),
		},
		{path: "/admin/portal", console: New(Options{Settings: settings})},
	}
	for _, page := range pages {
		got := do(t, page.console, page.path)
		if got.status != http.StatusOK || !strings.Contains(got.body, message) {
			t.Fatalf("%s missing unavailable state: %d %.120q", page.path, got.status, got.body)
		}
		if strings.Contains(got.body, `name="value:`) {
			t.Fatalf("%s rendered invented setting values", page.path)
		}
	}
}

func TestRuntimeSettingsReadFailureBlocksSave(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		view: SettingsView{Error: "Stored runtime settings are unavailable."},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})
	got := doPost(t, console, "/admin/configuration", url.Values{
		"key":                  {"portal.enabled"},
		"value:portal.enabled": {"false"},
	})
	if settings.calls != 0 {
		t.Fatalf("updates during unavailable read = %d", settings.calls)
	}
	if !strings.Contains(got.body, "Update failed. Please try again.") {
		t.Fatalf("missing safe update failure: %.120q", got.body)
	}
}
