package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestRegisterRoutesBindsStaticSection exercises the registerRoutes loop body
// that mounts a section handler for a nav item that is not a dynamic section.
// The default navItems are all dynamic, so this temporarily adds a static entry.
// It runs without t.Parallel so the global mutation stays within the sequential
// phase, before any parallel test body reads navItems.
func TestRegisterRoutesBindsStaticSection(t *testing.T) {
	orig := navItems
	defer func() { navItems = orig }()
	navItems = append(append([]NavItem(nil), orig...), NavItem{
		Title: "Extra", Path: "/admin/extra",
	})

	console := New(Options{})
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/admin/extra", nil,
	)
	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, req)

	// The route is registered, but /admin/extra is not in defaultSections, so
	// the section handler responds with 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestSectionHandlerRendersKnownSection(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/admin/crawl", nil,
	)
	New(Options{}).sectionHandler("/admin/crawl")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Crawler") {
		t.Fatal("expected the crawler section heading")
	}
}

func TestConsoleConfigUpdateUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), "/admin/configuration", url.Values{"key": {"x"}})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, configUnavailable) {
		t.Fatal("expected unavailable state on POST without a config source")
	}
}

func TestConsoleConfigBindUpdateErrorShowsGeneric(t *testing.T) {
	t.Parallel()

	binding := &fakeBinding{view: peerBindingView(), err: errors.New("backend detail")}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Binding: binding})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"form": {"binding"},
		"key":  {"bind.peer"},
	})
	if !strings.Contains(got.body, "Update failed. Please try again.") {
		t.Fatalf("expected generic bind error, got %s", got.body)
	}
	if strings.Contains(got.body, "backend detail") {
		t.Fatal("must not leak internal error detail")
	}
}

func TestConsoleConfigBindUpdateRejectedShowsReason(t *testing.T) {
	t.Parallel()

	binding := &fakeBinding{
		view:   peerBindingView(),
		result: BindResult{OK: false, Message: "Invalid port."},
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Binding: binding})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"form": {"binding"},
		"key":  {"bind.peer"},
		"port": {"nope"},
	})
	if !strings.Contains(got.body, "Invalid port.") {
		t.Fatalf("expected rejection reason, got %s", got.body)
	}
}

func TestConsoleConfigSettingsUpdateErrorShowsGeneric(t *testing.T) {
	t.Parallel()

	settings := &fakeSettings{
		view: portalSettingsView(true),
		err:  errors.New("backend detail"),
	}
	console := New(Options{Config: fakeConfig{view: ConfigView{}}, Settings: settings})

	got := doPost(t, console, "/admin/configuration", url.Values{
		"key":   {"portal.enabled"},
		"value": {"false"},
	})
	if !strings.Contains(got.body, "Update failed. Please try again.") {
		t.Fatalf("expected generic settings error, got %s", got.body)
	}
	if strings.Contains(got.body, "backend detail") {
		t.Fatal("must not leak internal error detail")
	}
}

func TestConsoleSecurityUpdateUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), "/admin/security", url.Values{"form": {"mint"}})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, securityUnavailable) {
		t.Fatal("expected unavailable state on POST without a security source")
	}
}

func TestConsoleSecurityUpdateUnknownAction(t *testing.T) {
	t.Parallel()

	console := New(Options{Security: &fakeSecurity{view: securityViewWithKey()}})
	got := doPost(t, console, "/admin/security", url.Values{"form": {"bogus"}})
	if !strings.Contains(got.body, "Unknown action.") {
		t.Fatalf("expected unknown-action notice, got %s", got.body)
	}
}

func TestConsoleSecurityMintErrorShowsGeneric(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view:    securityViewWithKey(),
		mintErr: errors.New("backend detail"),
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form":  {"mint"},
		"label": {"bot"},
	})
	if !strings.Contains(got.body, "Could not create the API key. Please try again.") {
		t.Fatalf("expected generic mint error, got %s", got.body)
	}
	if strings.Contains(got.body, "backend detail") {
		t.Fatal("must not leak internal error detail")
	}
}

func TestConsoleSecurityRevokeErrorShowsGeneric(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view:      securityViewWithKey(),
		revokeErr: errors.New("backend detail"),
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form": {"revoke"},
		"id":   {"abc123"},
	})
	if !strings.Contains(got.body, "Could not revoke the API key. Please try again.") {
		t.Fatalf("expected generic revoke error, got %s", got.body)
	}
}

func TestConsoleSecurityPasswordErrorShowsGeneric(t *testing.T) {
	t.Parallel()

	security := &fakeSecurity{
		view:  securityViewWithKey(),
		pwErr: errors.New("backend detail"),
	}
	console := New(Options{Security: security})

	got := doPost(t, console, "/admin/security", url.Values{
		"form":    {"password"},
		"new":     {"a"},
		"confirm": {"a"},
	})
	if !strings.Contains(got.body, "Could not change the password. Please try again.") {
		t.Fatalf("expected generic password error, got %s", got.body)
	}
}

func TestConsoleCrawlStartUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), "/admin/crawl", url.Values{
		"seeds": {"http://a.example"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, crawlUnavailable) {
		t.Fatal("expected unavailable state on POST without a crawl source")
	}
}

func TestConsoleRenderReportsTemplateError(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	rec := httptest.NewRecorder()
	console.render(
		context.Background(), rec, console.tpl.placeholder, "no-such-template", nil,
	)

	// render logs and swallows the execution error; the security headers are
	// still written before the failing ExecuteTemplate call.
	if rec.Header().Get("Content-Type") != htmlType {
		t.Fatal("expected html headers to be written before the render error")
	}
}
