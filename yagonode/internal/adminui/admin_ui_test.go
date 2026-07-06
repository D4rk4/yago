package adminui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
				Source:     "web",
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
	for _, want := range []string{"Local hit", "Web hit", ">web</span>", "result(s) for", "(local + peers)"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("search results missing %q", want)
		}
	}
	if strings.Contains(got.body, "[ddgs]") {
		t.Fatalf("admin search must not show the [ddgs] badge: %s", got.body)
	}
}

type recordingSearch struct {
	lastGlobal bool
	lastOffset int
	lastLimit  int
	results    SearchResults
}

func (r *recordingSearch) Search(_ context.Context, q SearchQuery) (SearchResults, error) {
	r.lastGlobal = q.Global
	r.lastOffset = q.Offset
	r.lastLimit = q.Limit

	return SearchResults{
		Query:        q.Query,
		Global:       q.Global,
		TotalResults: r.results.TotalResults,
		Results:      r.results.Results,
	}, nil
}

func TestConsoleSearchDefaultsToGlobalScope(t *testing.T) {
	t.Parallel()

	byDefault := &recordingSearch{}
	if got := do(
		t,
		New(Options{Search: byDefault}),
		"/admin/search?q=go",
	); got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !byDefault.lastGlobal {
		t.Fatal("search without an explicit scope should default to the peer network (global)")
	}

	localOnly := &recordingSearch{}
	do(t, New(Options{Search: localOnly}), "/admin/search?q=go&scope=local")
	if localOnly.lastGlobal {
		t.Fatal("scope=local should search only the local index")
	}
}

func TestConsoleSearchPaginationParsesPageIntoOffset(t *testing.T) {
	t.Parallel()

	rec := &recordingSearch{results: SearchResults{TotalResults: 200}}
	if got := do(
		t,
		New(Options{Search: rec}),
		"/admin/search?q=go&p=3",
	); got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if rec.lastOffset != 2*adminSearchPageSize || rec.lastLimit != adminSearchPageSize {
		t.Fatalf("offset=%d limit=%d, want offset=%d limit=%d",
			rec.lastOffset, rec.lastLimit, 2*adminSearchPageSize, adminSearchPageSize)
	}
}

func TestConsoleSearchPaginationRendersPrevAndNext(t *testing.T) {
	t.Parallel()

	rec := &recordingSearch{results: SearchResults{
		TotalResults: 200,
		Results:      []SearchResult{{Title: "hit", URL: "http://a/1"}},
	}}
	got := do(t, New(Options{Search: rec}), "/admin/search?q=go&scope=local&p=2")

	for _, want := range []string{
		"‹ Previous", "Next ›", "Page 2", `rel="prev"`, `rel="next"`,
		"p=1", "p=3", "scope=local",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("paginated admin results missing %q", want)
		}
	}
}

func TestConsoleSearchPaginationClampsPage(t *testing.T) {
	t.Parallel()

	rec := &recordingSearch{results: SearchResults{TotalResults: 5}}
	do(t, New(Options{Search: rec}), "/admin/search?q=go&p=99999")
	if rec.lastOffset != (adminSearchMaxPage-1)*adminSearchPageSize {
		t.Fatalf("over-max offset = %d, want %d",
			rec.lastOffset, (adminSearchMaxPage-1)*adminSearchPageSize)
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
		Available:       true,
		DHTOpen:         false,
		PublicReachable: true,
		BlockingReason:  "not enough peers",
		KnownPeers:      12,
		ReachablePeers:  5,
		OwnFlags: []NetworkFlag{
			{Name: "direct", Set: true},
			{Name: "remote-crawl", Set: false},
		},
		Gates: []NetworkGate{
			{Name: "connectedPeers", Open: false, Reason: "need 10"},
			{Name: "storage", Open: true},
		},
		Peers: []NetworkPeer{
			{
				Name:     "peerA",
				Hash:     "HHHHHH",
				Address:  "1.2.3.4:8090",
				Type:     "senior",
				Flags:    []string{"remote-index"},
				RWICount: 42,
				LastSeen: "2026-01-02T03:04:05Z",
				AgeDays:  3,
			},
		},
		Seedlists: []SeedlistEntry{
			{
				URL:        "https://seeds.example/seed.txt",
				Imported:   true,
				OK:         true,
				LastImport: "2026-07-04T09:00:00Z",
				Result:     "12 seeds",
			},
		},
	}
	got := do(t, New(Options{Network: fakeNetwork{snap: snap}}), "/admin/network")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"peerA", "1.2.3.4:8090", ">12<", ">5<", "connectedPeers",
		"not enough peers", "Closed", "senior", "remote-index", ">42<",
		"2026-01-02T03:04:05Z", "Reachable", "https://seeds.example/seed.txt",
		"Advertised to swarm", "direct: on", "remote-crawl: off",
	} {
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
	if !strings.Contains(got.body, "No peers yet.") {
		t.Fatal("expected empty peers state")
	}
	if !strings.Contains(got.body, "No seedlist URLs configured.") {
		t.Fatal("expected empty seedlist state")
	}
}

type fakeSeedlistRefresh struct {
	refreshed []string
	err       error
}

func (f *fakeSeedlistRefresh) RefreshSeedlist(_ context.Context, url string) error {
	f.refreshed = append(f.refreshed, url)

	return f.err
}

func networkWithSeedlist() fakeNetwork {
	return fakeNetwork{snap: NetworkStatus{
		Available: true,
		Seedlists: []SeedlistEntry{{URL: "https://seeds.example/seed.txt"}},
	}}
}

func TestConsoleSeedlistShowsRefreshButtonOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	without := do(t, New(Options{Network: networkWithSeedlist()}), "/admin/network")
	if strings.Contains(without.body, "Refresh now") {
		t.Fatal("no refresh action should render without a refresh source")
	}

	with := do(
		t,
		New(Options{Network: networkWithSeedlist(), SeedlistRefresh: &fakeSeedlistRefresh{}}),
		"/admin/network",
	)
	if !strings.Contains(with.body, "Refresh now") {
		t.Fatal("refresh action should render when a refresh source is wired")
	}
	if !strings.Contains(with.body, `action="/admin/network/seedlist/refresh"`) {
		t.Fatal("refresh form should post to the refresh endpoint")
	}
}

func TestConsoleSeedlistRefreshInvokesSource(t *testing.T) {
	t.Parallel()

	refresh := &fakeSeedlistRefresh{}
	got := doPost(
		t,
		New(Options{Network: networkWithSeedlist(), SeedlistRefresh: refresh}),
		seedlistRefreshPath,
		url.Values{"url": {"https://seeds.example/seed.txt"}},
	)
	if got.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", got.status)
	}
	if loc := got.header.Get("Location"); loc != networkPath {
		t.Fatalf("location %q, want %q", loc, networkPath)
	}
	if len(refresh.refreshed) != 1 || refresh.refreshed[0] != "https://seeds.example/seed.txt" {
		t.Fatalf("refreshed = %v", refresh.refreshed)
	}
}

func TestConsoleSeedlistRefreshWithoutSourceIsNotFound(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), seedlistRefreshPath, url.Values{"url": {"https://x/"}})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", got.status)
	}
}

func TestConsoleSeedlistRefreshRedirectsOnError(t *testing.T) {
	t.Parallel()

	refresh := &fakeSeedlistRefresh{err: errors.New("refresh failed")}
	got := doPost(
		t,
		New(Options{Network: networkWithSeedlist(), SeedlistRefresh: refresh}),
		seedlistRefreshPath,
		url.Values{"url": {"https://seeds.example/seed.txt"}},
	)
	if got.status != http.StatusSeeOther {
		t.Fatalf("a failed refresh should still redirect: status %d", got.status)
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

type fakePeerDetail struct {
	detail  PeerDetail
	ok      bool
	gotHash string
}

func (f *fakePeerDetail) PeerDetail(_ context.Context, hash string) (PeerDetail, bool) {
	f.gotHash = hash

	return f.detail, f.ok
}

func TestConsoleNetworkLinksToPeerDetail(t *testing.T) {
	t.Parallel()

	snap := NetworkStatus{
		Available: true,
		Peers:     []NetworkPeer{{Name: "peerA", Hash: "HHHHHHHHHHHH"}},
	}

	linked := do(
		t,
		New(Options{Network: fakeNetwork{snap: snap}, PeerDetail: &fakePeerDetail{ok: true}}),
		"/admin/network",
	)
	if !strings.Contains(linked.body, `href="/admin/network/peer?hash=HHHHHHHHHHHH"`) {
		t.Fatal("peer row should link to the detail page when a detail source is wired")
	}

	plain := do(t, New(Options{Network: fakeNetwork{snap: snap}}), "/admin/network")
	if strings.Contains(plain.body, "/admin/network/peer?hash=") {
		t.Fatal("peer rows must not link without a detail source")
	}
}

func TestConsoleNetworkPeerRendersDetail(t *testing.T) {
	t.Parallel()

	source := &fakePeerDetail{ok: true, detail: PeerDetail{
		Name: "peerA", Hash: "HHHHHHHHHHHH", Address: "1.2.3.4:8090", Version: "1.83",
		Type: "senior", Flags: []string{"remote-index"},
		RWIWords: 42, URLs: 1234, SentWords: 11, ReceivedURLs: 44,
	}}
	got := do(t, New(Options{PeerDetail: source}), "/admin/network/peer?hash=HHHHHHHHHHHH")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if source.gotHash != "HHHHHHHHHHHH" {
		t.Fatalf("handler read hash %q", source.gotHash)
	}
	for _, want := range []string{
		"peerA", "1.2.3.4:8090", "1.83", "senior", "remote-index",
		">42<", ">1234<", ">11<", ">44<",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("peer detail missing %q", want)
		}
	}
}

func TestConsoleNetworkPeerNotFound(t *testing.T) {
	t.Parallel()

	got := do(
		t,
		New(Options{PeerDetail: &fakePeerDetail{ok: false}}),
		"/admin/network/peer?hash=zzzzzzzzzzzz",
	)
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404 for an unknown peer", got.status)
	}
}

func TestConsoleNetworkPeerWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/network/peer?hash=HHHHHHHHHHHH")
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404 without a peer-detail source", got.status)
	}
}

type fakePeerBlock struct {
	blocked   []string
	unblocked []string
	err       error
}

func (f *fakePeerBlock) Block(_ context.Context, hash string) error {
	f.blocked = append(f.blocked, hash)

	return f.err
}

func (f *fakePeerBlock) Unblock(_ context.Context, hash string) error {
	f.unblocked = append(f.unblocked, hash)

	return f.err
}

func TestConsolePeerDetailShowsBlockControls(t *testing.T) {
	t.Parallel()

	open := &fakePeerDetail{ok: true, detail: PeerDetail{Hash: "HHHHHHHHHHHH"}}
	with := do(
		t,
		New(Options{PeerDetail: open, PeerBlock: &fakePeerBlock{}}),
		"/admin/network/peer?hash=HHHHHHHHHHHH",
	)
	if !strings.Contains(with.body, "Block peer") ||
		!strings.Contains(with.body, `value="block"`) {
		t.Fatal("an unblocked peer should offer a Block action when a block source is wired")
	}

	blockedSource := &fakePeerDetail{
		ok:     true,
		detail: PeerDetail{Hash: "HHHHHHHHHHHH", Blocked: true},
	}
	blocked := do(
		t,
		New(Options{PeerDetail: blockedSource, PeerBlock: &fakePeerBlock{}}),
		"/admin/network/peer?hash=HHHHHHHHHHHH",
	)
	if !strings.Contains(blocked.body, "Unblock peer") ||
		!strings.Contains(blocked.body, `value="unblock"`) {
		t.Fatal("a blocked peer should offer an Unblock action")
	}

	without := do(t, New(Options{PeerDetail: open}), "/admin/network/peer?hash=HHHHHHHHHHHH")
	if strings.Contains(without.body, "Block peer") {
		t.Fatal("no block controls should render without a block source")
	}
}

func TestConsolePeerBlockInvokesSource(t *testing.T) {
	t.Parallel()

	block := &fakePeerBlock{}
	got := doPost(t, New(Options{PeerBlock: block}), peerBlockPath, url.Values{
		"hash":   {"HHHHHHHHHHHH"},
		"action": {"block"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", got.status)
	}
	if loc := got.header.Get("Location"); loc != networkPath {
		t.Fatalf("location %q, want %q", loc, networkPath)
	}
	if len(block.blocked) != 1 || block.blocked[0] != "HHHHHHHHHHHH" {
		t.Fatalf("blocked = %v", block.blocked)
	}

	unblock := &fakePeerBlock{}
	got = doPost(t, New(Options{PeerBlock: unblock}), peerBlockPath, url.Values{
		"hash":   {"HHHHHHHHHHHH"},
		"action": {"unblock"},
	})
	if got.status != http.StatusSeeOther || len(unblock.unblocked) != 1 {
		t.Fatalf("unblock: status %d, calls %v", got.status, unblock.unblocked)
	}
}

func TestConsolePeerBlockRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{PeerBlock: &fakePeerBlock{}}), peerBlockPath, url.Values{
		"hash":   {"HHHHHHHHHHHH"},
		"action": {"delete"},
	})
	if got.status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for an unknown action", got.status)
	}
}

func TestConsolePeerBlockWithoutSourceIsNotFound(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), peerBlockPath, url.Values{
		"hash": {"HHHHHHHHHHHH"}, "action": {"block"},
	})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", got.status)
	}
}

func TestConsolePeerBlockRedirectsOnError(t *testing.T) {
	t.Parallel()

	block := &fakePeerBlock{err: errors.New("cannot block")}
	got := doPost(t, New(Options{PeerBlock: block}), peerBlockPath, url.Values{
		"hash": {"HHHHHHHHHHHH"}, "action": {"block"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("a failed block should still redirect: status %d", got.status)
	}
}

type fakePeerNews struct{ items []PeerNewsItem }

func (f fakePeerNews) PeerNews(context.Context) []PeerNewsItem { return f.items }

func TestConsoleNetworkRendersPeerNews(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Network: fakeNetwork{snap: NetworkStatus{Available: true}},
		PeerNews: fakePeerNews{items: []PeerNewsItem{{
			Category: "crwlstrt", Originator: "PEERHASH1234",
			Age: "3h", Detail: "startURL=http://x/",
		}}},
	})
	got := do(t, console, "/admin/network")
	for _, want := range []string{
		"Peer news", "crwlstrt", "PEERHASH1234", "3h", "startURL=http://x/",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("peer-news section missing %q", want)
		}
	}
}

func TestConsoleNetworkPeerNewsEmptyState(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Network:  fakeNetwork{snap: NetworkStatus{Available: true}},
		PeerNews: fakePeerNews{},
	})
	got := do(t, console, "/admin/network")
	if !strings.Contains(got.body, "No peer news received yet.") {
		t.Fatal("expected the peer-news empty state when the source has no items")
	}
}

func TestConsoleNetworkHidesPeerNewsWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(
		t,
		New(Options{Network: fakeNetwork{snap: NetworkStatus{Available: true}}}),
		"/admin/network",
	)
	if strings.Contains(got.body, "Peer news") {
		t.Fatal("peer-news section should be hidden without a source")
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

func TestConsoleLogsFiltersBySeverityAndCategory(t *testing.T) {
	t.Parallel()

	entries := []LogEntry{
		{Time: "t1", Severity: "warn", Category: "security", Name: "login.failed", Message: "bad"},
		{Time: "t2", Severity: "info", Category: "config", Name: "node.started", Message: "up"},
	}
	console := New(Options{Logs: fakeLogs{entries: entries}})

	sev := do(t, console, "/admin/logs?severity=warn")
	if !strings.Contains(sev.body, "login.failed") || strings.Contains(sev.body, "node.started") {
		t.Fatal("severity filter should keep only warn events")
	}
	if !strings.Contains(sev.body, `value="warn" selected`) {
		t.Fatal("severity dropdown should pre-select the active filter")
	}
	if !strings.Contains(sev.body, "severity=warn") || !strings.Contains(sev.body, "filtered") {
		t.Fatal("refresh URL should carry the filter and mark the view filtered")
	}

	cat := do(t, console, "/admin/logs?category=config")
	if !strings.Contains(cat.body, "node.started") || strings.Contains(cat.body, "login.failed") {
		t.Fatal("category filter should keep only config events")
	}
	if !strings.Contains(cat.body, `value="security"`) ||
		!strings.Contains(cat.body, `value="config"`) {
		t.Fatal("category dropdown should offer every observed category")
	}
}

func TestConsoleLogsEventsPartialHonorsFilter(t *testing.T) {
	t.Parallel()

	entries := []LogEntry{
		{Time: "t1", Severity: "error", Category: "dht", Name: "alpha", Message: "m1"},
		{Time: "t2", Severity: "info", Category: "dht", Name: "beta", Message: "m2"},
	}
	got := do(
		t,
		New(Options{Logs: fakeLogs{entries: entries}}),
		"/admin/logs/events?severity=error",
	)
	if !strings.Contains(got.body, "alpha") || strings.Contains(got.body, "beta") {
		t.Fatal("events partial should honor the severity filter")
	}
}

func TestLogFilterHelpers(t *testing.T) {
	t.Parallel()

	entries := []LogEntry{
		{Severity: "warn", Category: "b"},
		{Severity: "info", Category: "a"},
		{Severity: "warn", Category: "a"},
		{Severity: "info", Category: ""},
	}
	if cats := distinctLogCategories(entries); len(cats) != 2 || cats[0] != "a" || cats[1] != "b" {
		t.Fatalf("categories = %v, want sorted [a b] without blanks", cats)
	}
	if got := filterLogEntries(entries, "", "a"); len(got) != 2 {
		t.Fatalf("category-only filter = %d, want 2", len(got))
	}
	if got := filterLogEntries(entries, "warn", "a"); len(got) != 1 {
		t.Fatalf("combined filter = %d, want 1", len(got))
	}
	if got := filterLogEntries(entries, "", ""); len(got) != len(entries) {
		t.Fatal("an empty filter returns every entry")
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
		45:    "45s",
		59:    "59s",
		60:    "1m 0s",
		3600:  "1h 0m 0s",
		3661:  "1h 1m 1s",
		90061: "1d 1h 1m 1s",
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

type fakeCrawl struct {
	got    CrawlStart
	result CrawlDispatch
	err    error
}

func (f *fakeCrawl) Start(_ context.Context, start CrawlStart) (CrawlDispatch, error) {
	f.got = start

	return f.result, f.err
}

func doPost(t *testing.T, console *Console, path string, form url.Values) capture {
	t.Helper()

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		path,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func TestConsoleCrawlUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/crawl")
	if !strings.Contains(got.body, crawlUnavailable) {
		t.Fatal("expected unavailable state without a crawl source")
	}
}

func TestConsoleCrawlRendersForm(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Crawl: &fakeCrawl{}}), "/admin/crawl")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{`name="seeds"`, `action="/admin/crawl"`, `name="csrf_token"`, `name="maxDepth"`} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("crawl form missing %q", want)
		}
	}
}

func TestConsoleCrawlStartDispatches(t *testing.T) {
	t.Parallel()

	crawl := &fakeCrawl{result: CrawlDispatch{ProfileHandle: "PH123", Seeds: 2}}
	got := doPost(t, New(Options{Crawl: crawl}), "/admin/crawl", url.Values{
		"seeds":    {"http://a.example\nhttp://b.example"},
		"mode":     {"url"},
		"scope":    {"domain"},
		"maxDepth": {"3"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if len(crawl.got.Seeds) != 2 {
		t.Fatalf("seeds = %v", crawl.got.Seeds)
	}
	if crawl.got.MaxDepth != 3 {
		t.Fatalf("maxDepth = %d", crawl.got.MaxDepth)
	}
	if !strings.Contains(got.body, "Crawl accepted") || !strings.Contains(got.body, "PH123") {
		t.Fatalf("expected acceptance, got %s", got.body)
	}
}

func TestConsoleCrawlStartRejectsEmptySeeds(t *testing.T) {
	t.Parallel()

	crawl := &fakeCrawl{}
	got := doPost(t, New(Options{Crawl: crawl}), "/admin/crawl", url.Values{"seeds": {"   \n  "}})
	if !strings.Contains(got.body, "at least one seed") {
		t.Fatalf("expected empty-seed error, got %s", got.body)
	}
	if len(crawl.got.Seeds) != 0 {
		t.Fatal("dispatcher should not be called for empty seeds")
	}
}

func TestConsoleCrawlStartShowsError(t *testing.T) {
	t.Parallel()

	crawl := &fakeCrawl{err: errors.New("invalid crawl profile: bad regex")}
	got := doPost(
		t,
		New(Options{Crawl: crawl}),
		"/admin/crawl",
		url.Values{"seeds": {"http://a.example"}, "urlMustMatch": {"("}, "showExpert": {"on"}},
	)
	// The dispatcher's validation message is surfaced so the operator can fix the
	// offending expert field, and the expert panel stays open on redisplay.
	if !strings.Contains(got.body, "Crawl start failed") ||
		!strings.Contains(got.body, "bad regex") {
		t.Fatalf("expected the validation detail, got %s", got.body)
	}
	if !strings.Contains(got.body, "<details class=\"cds-expert\" open>") {
		t.Fatal("expert panel should stay open after a validation error")
	}
}

func TestConsoleCrawlStartPassesExpertFields(t *testing.T) {
	t.Parallel()

	crawl := &fakeCrawl{result: CrawlDispatch{ProfileHandle: "PH", Seeds: 1}}
	got := doPost(t, New(Options{Crawl: crawl}), "/admin/crawl", url.Values{
		"seeds":               {"http://a.example"},
		"mode":                {"url"},
		"scope":               {"domain"},
		"maxDepth":            {"2"},
		"urlMustMatch":        {`https?://a\.example/.*`},
		"urlMustNotMatch":     {`.*\.pdf$`},
		"indexMustMatch":      {".*"},
		"indexMustNotMatch":   {`.*/private/.*`},
		"maxPagesPerHost":     {"50"},
		"crawlDelay":          {"2s"},
		"recrawlIfOlder":      {"24h"},
		"allowQueryURLs":      {"on"},
		"followNoFollowLinks": {"on"},
		"showExpert":          {"on"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	switch {
	case crawl.got.URLMustMatch != `https?://a\.example/.*`:
		t.Fatalf("urlMustMatch = %q", crawl.got.URLMustMatch)
	case crawl.got.URLMustNotMatch != `.*\.pdf$`:
		t.Fatalf("urlMustNotMatch = %q", crawl.got.URLMustNotMatch)
	case crawl.got.IndexURLMustNotMatch != `.*/private/.*`:
		t.Fatalf("indexMustNotMatch = %q", crawl.got.IndexURLMustNotMatch)
	case crawl.got.MaxPagesPerHost != 50:
		t.Fatalf("maxPagesPerHost = %d", crawl.got.MaxPagesPerHost)
	case !crawl.got.AllowQueryURLs || !crawl.got.FollowNoFollowLinks:
		t.Fatalf("checkboxes not captured: %+v", crawl.got)
	case crawl.got.RecrawlIfOlder != "24h" || crawl.got.CrawlDelay != "2s":
		t.Fatalf("durations not captured: %+v", crawl.got)
	}
	if !strings.Contains(got.body, "Expert options") {
		t.Fatal("expert panel missing from the response")
	}
}

type fakeConfig struct{ view ConfigView }

func (f fakeConfig) Config(context.Context) ConfigView { return f.view }

func TestConsoleConfigUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/configuration")
	if !strings.Contains(got.body, configUnavailable) {
		t.Fatal("expected unavailable state without a config source")
	}
}

func TestConsoleConfigRendersGroups(t *testing.T) {
	t.Parallel()

	view := ConfigView{Groups: []ConfigGroup{
		{Title: "Search", Settings: []ConfigSetting{
			{Name: "Search API key", Value: "Configured"},
			{Name: "Require API key", Value: "Yes"},
		}},
	}}
	got := do(t, New(Options{Config: fakeConfig{view: view}}), "/admin/configuration")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"Search", "Search API key", "Configured", "Require API key"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("config view missing %q", want)
		}
	}
}
