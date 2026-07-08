package adminui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type portalFakeSettings struct {
	items  []SettingItem
	got    SettingsChange
	result SettingsResult
	err    error
}

func (f *portalFakeSettings) Settings(context.Context) SettingsView {
	return SettingsView{Items: f.items}
}

func (f *portalFakeSettings) Update(
	_ context.Context,
	change SettingsChange,
) (SettingsResult, error) {
	f.got = change

	return f.result, f.err
}

func portalTestSettings() *portalFakeSettings {
	return &portalFakeSettings{
		items: []SettingItem{
			{Key: "portal.enabled", Title: "Public portal enabled", Category: "Public portal"},
			{Key: "https.redirect", Title: "Redirect HTTP to HTTPS", Category: "Public portal"},
			{Key: "web.robots.policy", Title: "Robots policy", Category: "Public portal"},
			{Key: "peer.name", Title: "Peer name", Category: "Network & peers"},
		},
		result: SettingsResult{OK: true, Message: "Saved."},
	}
}

// TestPortalSectionRendersTabsAndSubset is the ADR-0033 slice-1 acceptance: the
// Public portal page shows the three tabs and, on the Configuration tab, only
// the portal-facing settings, not foreign-category items.
func TestPortalSectionRendersTabsAndSubset(t *testing.T) {
	t.Parallel()

	console := New(Options{Settings: portalTestSettings()})
	got := do(t, console, "/admin/portal")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d", got.status)
	}
	for _, want := range []string{
		"Configuration", "Portal design", "Results design",
		"portal.enabled", "web.robots.policy",
		`action="/admin/portal"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("portal page missing %q", want)
		}
	}
	if strings.Contains(got.body, `name="key" value="peer.name"`) {
		t.Fatal("foreign-category setting leaked into the portal section")
	}
}

func TestPortalNavOrderBetweenActivityAndAutocrawler(t *testing.T) {
	t.Parallel()

	console := New(Options{Settings: portalTestSettings()})
	got := do(t, console, "/admin/portal")
	activity := strings.Index(got.body, `cds-nav__label">Activity</span>`)
	portal := strings.Index(got.body, `cds-nav__label">Public portal</span>`)
	autocrawler := strings.Index(got.body, `cds-nav__label">Autocrawler</span>`)
	if activity < 0 || activity >= portal || portal >= autocrawler {
		t.Fatalf(
			"nav order wrong: activity@%d portal@%d autocrawler@%d",
			activity,
			portal,
			autocrawler,
		)
	}
}

// TestConfigurationSheetHidesPortalCategory pins the split introduced with the
// dedicated Public portal page: the flat Configuration sheet must not render a
// second, competing form for the portal-facing keys.
func TestConfigurationSheetHidesPortalCategory(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Config:   fakeConfig{view: ConfigView{}},
		Settings: portalTestSettings(),
	})
	got := do(t, console, "/admin/configuration")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d", got.status)
	}
	if strings.Contains(got.body, `id="tab-public-portal"`) {
		t.Error("Configuration must not render a Public portal tab")
	}
	if strings.Contains(got.body, `value="portal.enabled"`) ||
		strings.Contains(got.body, `name="key" value="portal.greeting"`) {
		t.Error("portal keys must not be editable from Configuration")
	}
	if !strings.Contains(got.body, "Network &amp; peers") {
		t.Error("the remaining categories must still render")
	}
}

func TestPortalUpdateAcceptsOwnKeysOnly(t *testing.T) {
	t.Parallel()

	settings := portalTestSettings()
	console := New(Options{Settings: settings})

	posted := doPost(t, console, "/admin/portal", url.Values{
		"key": {"https.redirect"}, "value:https.redirect": {"true"},
	})
	if posted.status != http.StatusOK || !strings.Contains(posted.body, "1 setting updated.") {
		t.Fatalf("update = %d %.60q", posted.status, posted.body)
	}
	if settings.got.Key != "https.redirect" || settings.got.Value != "true" {
		t.Fatalf("change = %+v", settings.got)
	}

	foreign := doPost(t, console, "/admin/portal", url.Values{
		"key": {"peer.name"}, "value:peer.name": {"sneaky"},
	})
	if foreign.status != http.StatusNotFound {
		t.Fatalf("foreign key = %d, want 404", foreign.status)
	}
}

func TestPortalUpdateSurfacesSaveFailure(t *testing.T) {
	t.Parallel()

	settings := portalTestSettings()
	settings.err = context.DeadlineExceeded
	console := New(Options{Settings: settings})

	posted := doPost(t, console, "/admin/portal", url.Values{
		"key": {"portal.enabled"}, "value:portal.enabled": {"true"},
	})
	if posted.status != http.StatusOK ||
		!strings.Contains(posted.body, "Update failed. Please try again.") {
		t.Fatalf("save failure = %d %.80q", posted.status, posted.body)
	}
}

// TestPortalDesignTabsRenderPlaceholders pins the no-store degradation: a
// deployment without a theme store keeps the design tabs, explains why the
// editors cannot load, ships no editor script, and keeps the strict CSP.
func TestPortalDesignTabsRenderPlaceholders(t *testing.T) {
	t.Parallel()

	console := New(Options{Settings: portalTestSettings()})
	got := do(t, console, "/admin/portal")
	for _, want := range []string{
		`id="panel-portal-design"`,
		`id="panel-portal-results"`,
		"design store is not available",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("portal design placeholder missing %q", want)
		}
	}
	if strings.Contains(got.body, "portal_designer.js") {
		t.Error("no editor script may load without a theme store")
	}
	if got.header.Get("Content-Security-Policy") != contentPol {
		t.Errorf("placeholder page must keep the strict CSP: %q",
			got.header.Get("Content-Security-Policy"))
	}
}

func TestPortalUnavailableWithoutSettings(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	got := do(t, console, "/admin/portal")
	if got.status != http.StatusOK || !strings.Contains(got.body, "not available") {
		t.Fatalf("unavailable page = %d", got.status)
	}
	posted := doPost(t, console, "/admin/portal", url.Values{"key": {"portal.enabled"}})
	if posted.status != http.StatusNotFound {
		t.Fatalf("update without settings = %d, want 404", posted.status)
	}
}
