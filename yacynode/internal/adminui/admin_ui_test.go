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

type fakeOverview struct{ snap Overview }

func (f fakeOverview) Overview(context.Context) Overview { return f.snap }

func sampleOverview() Overview {
	return Overview{
		PeerName:      "test-peer",
		PeerHash:      "ABCDEFGHIJKL",
		PeerType:      "senior",
		Version:       "1.2.3",
		UptimeSeconds: 90061,
		Documents:     42,
		Words:         100,
		KnownPeers:    7,
		SentWords:     5,
		ReceivedWords: 6,
		SentURLs:      3,
		ReceivedURLs:  4,
	}
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

	console := New(Options{})
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

	got := do(t, New(Options{}), "/admin/search")
	if !strings.Contains(got.header.Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatal("CSP must allow same-origin htmx requests")
	}
	if got.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}

func TestConsoleOverviewUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	console := New(Options{})

	page := do(t, console, "/admin/overview")
	if !strings.Contains(page.body, "cds-empty") {
		t.Fatal("expected unavailable state without an overview source")
	}
	if !strings.Contains(page.body, overviewUnavailable) {
		t.Fatal("expected unavailable message")
	}

	metrics := do(t, console, "/admin/overview/metrics")
	if metrics.status != http.StatusNotFound {
		t.Fatalf("metrics without source: status %d", metrics.status)
	}
}

func TestConsoleOverviewRendersLiveStatus(t *testing.T) {
	t.Parallel()

	console := New(Options{Overview: fakeOverview{snap: sampleOverview()}})

	got := do(t, console, "/admin/overview")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"test-peer", "ABCDEFGHIJKL", "senior", ">42<", "1d 1h 1m", "overview-metrics"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("overview missing %q", want)
		}
	}
	if !strings.Contains(got.body, "<header") {
		t.Fatal("full page should include the shell header")
	}
}

func TestConsoleOverviewMetricsPartialIsFragment(t *testing.T) {
	t.Parallel()

	console := New(Options{Overview: fakeOverview{snap: sampleOverview()}})

	got := do(t, console, "/admin/overview/metrics")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, `id="overview-metrics"`) {
		t.Fatal("fragment must be the self-refreshing region")
	}
	if !strings.Contains(got.body, ">42<") {
		t.Fatal("fragment missing document count")
	}
	if strings.Contains(got.body, "<header") || strings.Contains(got.body, "<nav") {
		t.Fatal("partial must not include the full shell")
	}
}

func TestConsoleIndexRedirectsToOverview(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/")
	if got.status != http.StatusFound {
		t.Fatalf("status %d", got.status)
	}
	if loc := got.header.Get("Location"); loc != "/admin/overview" {
		t.Fatalf("location %q", loc)
	}
}

func TestConsoleServesEmbeddedAssets(t *testing.T) {
	t.Parallel()

	console := New(Options{})

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

	got := do(t, New(Options{}), "/admin/does-not-exist")
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d", got.status)
	}
}

func TestSectionHandlerRejectsUnknownPath(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/ghost", nil)
	New(Options{}).sectionHandler("/admin/ghost")(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHumanDuration(t *testing.T) {
	t.Parallel()

	cases := map[int]string{
		0:     "0s",
		59:    "0m",
		60:    "1m",
		3661:  "1h 1m",
		90061: "1d 1h 1m",
	}
	for seconds, want := range cases {
		if got := humanDuration(seconds); got != want {
			t.Fatalf("humanDuration(%d) = %q, want %q", seconds, got, want)
		}
	}
}
