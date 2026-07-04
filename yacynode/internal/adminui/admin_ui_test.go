package adminui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capture struct {
	status int
	header http.Header
	body   string
}

func do(t *testing.T, console *Console, path string) capture {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, req)

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return capture{status: resp.StatusCode, header: resp.Header, body: string(body)}
}

func TestConsoleRendersEveryNavRoute(t *testing.T) {
	t.Parallel()

	console := New()
	for _, item := range navItems {
		got := do(t, console, item.Path)
		if got.status != http.StatusOK {
			t.Fatalf("%s: status %d", item.Path, got.status)
		}
		if ct := got.header.Get("Content-Type"); ct != htmlType {
			t.Fatalf("%s: content-type %q", item.Path, ct)
		}
		if !strings.Contains(got.body, `aria-current="page"`) {
			t.Fatalf("%s: no active nav item", item.Path)
		}
		if !strings.Contains(got.body, item.Title) {
			t.Fatalf("%s: heading %q missing", item.Path, item.Title)
		}
		if !strings.Contains(got.body, "GNU AGPL") {
			t.Fatalf("%s: AGPL notice missing", item.Path)
		}
	}
}

func TestConsoleSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	got := do(t, New(), "/admin/overview")
	if got.header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing Content-Security-Policy")
	}
	if got.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}

func TestConsoleOverviewIsAvailable(t *testing.T) {
	t.Parallel()

	got := do(t, New(), "/admin/overview")
	if !strings.Contains(got.body, "Operator console") {
		t.Fatal("overview welcome missing")
	}
	if !strings.Contains(got.body, "/search") {
		t.Fatal("overview should link to public search")
	}
}

func TestConsoleUnavailableSectionShowsEmptyState(t *testing.T) {
	t.Parallel()

	got := do(t, New(), "/admin/network")
	if !strings.Contains(got.body, "cds-empty") {
		t.Fatal("expected controlled unavailable state")
	}
	if !strings.Contains(got.body, "Peers, seed lists") {
		t.Fatal("expected section blurb")
	}
}

func TestConsoleIndexRedirectsToOverview(t *testing.T) {
	t.Parallel()

	got := do(t, New(), "/admin/")
	if got.status != http.StatusFound {
		t.Fatalf("status %d", got.status)
	}
	if loc := got.header.Get("Location"); loc != "/admin/overview" {
		t.Fatalf("location %q", loc)
	}
}

func TestConsoleServesEmbeddedAssets(t *testing.T) {
	t.Parallel()

	console := New()

	css := do(t, console, "/admin/assets/carbon.css")
	if css.status != http.StatusOK {
		t.Fatalf("css status %d", css.status)
	}
	if css.header.Get("Cache-Control") == "" {
		t.Fatal("assets should be cacheable")
	}
	if !strings.Contains(css.body, "--cds-interactive") {
		t.Fatal("carbon tokens missing")
	}

	js := do(t, console, "/admin/assets/htmx.min.js")
	if js.status != http.StatusOK {
		t.Fatalf("htmx status %d", js.status)
	}
	if !strings.Contains(js.body, "htmx") {
		t.Fatal("htmx payload missing")
	}
}

func TestConsoleUnknownSectionIsNotFound(t *testing.T) {
	t.Parallel()

	got := do(t, New(), "/admin/does-not-exist")
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d", got.status)
	}
}

func TestSectionHandlerRejectsUnknownPath(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/ghost", nil)
	New().sectionHandler("/admin/ghost")(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}
