package adminui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSearch struct {
	results SearchResults
	err     error
}

func (f fakeSearch) Search(context.Context, SearchQuery) (SearchResults, error) {
	return f.results, f.err
}

type fakeIndex struct{ snap IndexStats }

func (f fakeIndex) Index(context.Context) IndexStats { return f.snap }

type fakeNetwork struct{ snap NetworkStatus }

func (f fakeNetwork) Network(context.Context) NetworkStatus { return f.snap }

type fakeLogs struct{ entries []LogEntry }

func (f fakeLogs) Logs(context.Context) []LogEntry { return f.entries }

func sampleResults() SearchResults {
	return SearchResults{
		Query:        "go",
		Global:       true,
		TotalResults: 2,
		Results: []SearchResult{
			{
				Title:      "Local hit",
				URL:        "http://a.example/1",
				DisplayURL: "a.example/1",
				Snippet:    "s",
			},
			{
				Title:      "Web hit",
				URL:        "http://b.example/2",
				DisplayURL: "b.example/2",
				Source:     "ddgs",
				Marked:     true,
			},
		},
	}
}

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

func TestConsoleSearchFormRendersWithoutQuery(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Search: fakeSearch{}}), "/admin/search")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, `name="q"`) {
		t.Fatal("search form missing")
	}
	if strings.Contains(got.body, "result(s) for") {
		t.Fatal("no results should render before a query")
	}
}

func TestConsoleSearchRendersResultsWithMarker(t *testing.T) {
	t.Parallel()

	console := New(Options{Search: fakeSearch{results: sampleResults()}})
	got := do(t, console, "/admin/search?q=go&scope=global")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"Local hit", "Web hit", "[ddgs]", "result(s) for", "(local + peers)"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("search results missing %q", want)
		}
	}
}

func TestConsoleSearchUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/search")
	if !strings.Contains(got.body, "cds-empty") {
		t.Fatal("expected unavailable state without a search source")
	}
	if !strings.Contains(got.body, searchUnavailable) {
		t.Fatal("expected unavailable message")
	}
}

func TestConsoleSearchErrorIsGeneric(t *testing.T) {
	t.Parallel()

	console := New(Options{Search: fakeSearch{err: errors.New("backend detail")}})
	got := do(t, console, "/admin/search?q=go")
	if !strings.Contains(got.body, "Search failed.") {
		t.Fatal("expected generic error notification")
	}
	if strings.Contains(got.body, "backend detail") {
		t.Fatal("must not leak internal error detail")
	}
}

func TestConsoleIndexRendersStats(t *testing.T) {
	t.Parallel()

	source := fakeIndex{snap: IndexStats{Available: true, Documents: 99, Backend: "bleve"}}
	got := do(t, New(Options{Index: source}), "/admin/index")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{">99<", "bleve", "Indexed documents"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("index section missing %q", want)
		}
	}
}

func TestConsoleIndexUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/index")
	if !strings.Contains(got.body, "cds-empty") {
		t.Fatal("expected unavailable state without an index source")
	}
	if !strings.Contains(got.body, indexUnavailable) {
		t.Fatal("expected unavailable message")
	}
}

func TestConsoleNetworkRendersStatus(t *testing.T) {
	t.Parallel()

	snap := NetworkStatus{
		Available:      true,
		DHTOpen:        false,
		BlockingReason: "not enough peers",
		KnownPeers:     12,
		ReachablePeers: 5,
		Gates: []NetworkGate{
			{Name: "connectedPeers", Open: false, Reason: "need 10"},
			{Name: "storage", Open: true},
		},
		Peers: []NetworkPeer{
			{Name: "peerA", Hash: "HHHHHH", Address: "1.2.3.4:8090", AgeDays: 3},
		},
	}
	got := do(t, New(Options{Network: fakeNetwork{snap: snap}}), "/admin/network")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"peerA", "1.2.3.4:8090", ">12<", ">5<", "connectedPeers", "not enough peers", "Closed"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("network section missing %q", want)
		}
	}
}

func TestConsoleNetworkEmptyStates(t *testing.T) {
	t.Parallel()

	got := do(
		t,
		New(Options{Network: fakeNetwork{snap: NetworkStatus{Available: true}}}),
		"/admin/network",
	)
	if !strings.Contains(got.body, "No gate data.") {
		t.Fatal("expected empty gate row")
	}
	if !strings.Contains(got.body, "No reachable peers yet.") {
		t.Fatal("expected empty peers state")
	}
}

func TestConsoleNetworkUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/network")
	if !strings.Contains(got.body, "cds-empty") {
		t.Fatal("expected unavailable state without a network source")
	}
	if !strings.Contains(got.body, networkUnavailable) {
		t.Fatal("expected unavailable message")
	}
}

func TestConsoleLogsRendersEvents(t *testing.T) {
	t.Parallel()

	entries := []LogEntry{
		{
			Time:     "2026-07-04T00:00:00Z",
			Severity: "warn",
			Category: "security",
			Name:     "login.failed",
			Message:  "bad password",
		},
	}
	got := do(t, New(Options{Logs: fakeLogs{entries: entries}}), "/admin/logs")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"login.failed", "bad password", "security", "cds-tag--warn", `id="logs-table"`} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("logs section missing %q", want)
		}
	}
}

func TestConsoleLogsPartialIsFragment(t *testing.T) {
	t.Parallel()

	entries := []LogEntry{
		{Time: "t", Severity: "info", Category: "config", Name: "node.started", Message: "up"},
	}
	got := do(t, New(Options{Logs: fakeLogs{entries: entries}}), "/admin/logs/events")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, `id="logs-table"`) {
		t.Fatal("fragment must be the self-refreshing region")
	}
	if strings.Contains(got.body, "<header") || strings.Contains(got.body, "<nav") {
		t.Fatal("partial must not include the full shell")
	}
}

func TestConsoleLogsEmptyState(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Logs: fakeLogs{}}), "/admin/logs")
	if !strings.Contains(got.body, "No events recorded yet.") {
		t.Fatal("expected empty events state")
	}
}

func TestConsoleLogsUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	console := New(Options{})

	page := do(t, console, "/admin/logs")
	if !strings.Contains(page.body, "cds-empty") {
		t.Fatal("expected unavailable state without a logs source")
	}
	if !strings.Contains(page.body, logsUnavailable) {
		t.Fatal("expected unavailable message")
	}

	events := do(t, console, "/admin/logs/events")
	if events.status != http.StatusNotFound {
		t.Fatalf("logs partial without source: status %d", events.status)
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

func TestLayoutRendersSignOutWithCSRF(t *testing.T) {
	console := New(Options{})
	var buf bytes.Buffer
	if err := console.tpl.placeholder.ExecuteTemplate(&buf, "layout", pageData{
		AppName: appName, Nav: navItems, CSRF: "tok-123",
		Section: sectionView{Heading: "Overview"},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `action="/admin/logout"`) ||
		!strings.Contains(out, `value="tok-123"`) {
		t.Fatalf("sign-out form missing: %s", out)
	}
}

func TestLayoutOmitsSignOutWithoutCSRF(t *testing.T) {
	console := New(Options{})
	var buf bytes.Buffer
	if err := console.tpl.placeholder.ExecuteTemplate(&buf, "layout", pageData{
		AppName: appName, Nav: navItems,
		Section: sectionView{Heading: "Overview"},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), "/admin/logout") {
		t.Fatalf("sign-out form should be absent without a CSRF token")
	}
}
