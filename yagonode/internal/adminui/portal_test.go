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

func TestPortalNavOrderBetweenConfigurationAndSecurity(t *testing.T) {
	t.Parallel()

	console := New(Options{Settings: portalTestSettings()})
	got := do(t, console, "/admin/portal")
	config := strings.Index(got.body, `cds-nav__label">Configuration</span>`)
	portal := strings.Index(got.body, `cds-nav__label">Public portal</span>`)
	security := strings.Index(got.body, `cds-nav__label">Security</span>`)
	if config < 0 || config >= portal || portal >= security {
		t.Fatalf("nav order wrong: config@%d portal@%d security@%d", config, portal, security)
	}
}

func TestPortalUpdateAcceptsOwnKeysOnly(t *testing.T) {
	t.Parallel()

	settings := portalTestSettings()
	console := New(Options{Settings: settings})

	posted := doPost(t, console, "/admin/portal", url.Values{
		"key": {"https.redirect"}, "value": {"true"},
	})
	if posted.status != http.StatusOK || !strings.Contains(posted.body, "Saved.") {
		t.Fatalf("update = %d %.60q", posted.status, posted.body)
	}
	if settings.got.Key != "https.redirect" || settings.got.Value != "true" {
		t.Fatalf("change = %+v", settings.got)
	}

	foreign := doPost(t, console, "/admin/portal", url.Values{
		"key": {"peer.name"}, "value": {"sneaky"},
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
		"key": {"portal.enabled"}, "value": {"true"},
	})
	if posted.status != http.StatusOK ||
		!strings.Contains(posted.body, "Update failed. Please try again.") {
		t.Fatalf("save failure = %d %.80q", posted.status, posted.body)
	}
}

func TestPortalDesignTabsRenderPlaceholders(t *testing.T) {
	t.Parallel()

	console := New(Options{Settings: portalTestSettings()})
	got := do(t, console, "/admin/portal")
	for _, want := range []string{
		`id="panel-portal-design"`,
		`id="panel-portal-results"`,
		"arrive in a following slice",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("portal design placeholder missing %q", want)
		}
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
