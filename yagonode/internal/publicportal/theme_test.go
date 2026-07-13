package publicportal

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var errFailingSource = errors.New("search backend down")

type fakeTheme struct {
	html     string
	ok       bool
	gotPage  string
	gotView  map[string]any
	rendered int
}

func (f *fakeTheme) Render(_ context.Context, page string, view map[string]any) (string, bool) {
	f.gotPage = page
	f.gotView = view
	f.rendered++

	return f.html, f.ok
}

func richResults() SearchResults {
	return SearchResults{
		Query:         "cats",
		TotalResults:  12,
		LocalCount:    5,
		PeerCount:     4,
		WebCount:      3,
		PeersFailed:   1,
		Recovered:     true,
		DidYouMean:    "cat",
		DidYouMeanURL: "/?q=cat",
		Results: []SearchResult{{
			Title:       "First <hit>",
			URL:         "https://example.com/a",
			DisplayURL:  "example.com/a",
			Snippet:     "plain",
			SnippetHTML: template.HTML("high <mark>light</mark>"),
			Host:        "example.com",
			Date:        "2026-07-01",
			SizeName:    "12 kB",
			CachedURL:   "/cache?u=1",
			Provenance:  "local",
			FaviconURL:  "/favicon?host=example.com",
			Images: []ResultImage{{
				ProxyURL: "/img?u=1",
				Alt:      "an image",
				PageURL:  "https://example.com/a",
			}},
		}},
		Facets: []FacetGroup{
			{
				Title: "Hosts",
				Items: []FacetItem{
					{Label: "example.com", Count: 5, URL: "/?q=cats+site%3Aexample.com"},
				},
			},
		},
	}
}

func TestHitViewMarksDDGSProvenance(t *testing.T) {
	t.Parallel()

	view := hitView(SearchResult{Provenance: "ddgs"})
	if view["provenance"] != "ddgs" || view["provenanceLabel"] != "web" {
		t.Fatalf("DDGS provenance view = %#v", view)
	}

	local := hitView(SearchResult{Provenance: "local"})
	if local["provenanceLabel"] != "local" {
		t.Fatalf("local provenance label = %#v", local["provenanceLabel"])
	}
}

func TestThemedPortalServesOperatorPage(t *testing.T) {
	t.Parallel()

	theme := &fakeTheme{html: "<html>themed results</html>", ok: true}
	portal := New(&fakeSource{results: richResults()}, true)
	portal.SetTheme(theme)

	status, body := get(t, portal, "/?q=cats&p=1")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if body != "<html>themed results</html>" {
		t.Fatalf("themed body not served: %q", body)
	}
	if theme.gotPage != themePageResults {
		t.Errorf("page = %q, want results", theme.gotPage)
	}
}

func TestThemedPortalPicksSearchPageWithoutQuery(t *testing.T) {
	t.Parallel()

	theme := &fakeTheme{html: "<html>themed home</html>", ok: true}
	portal := New(&fakeSource{}, false)
	portal.SetTheme(theme)

	if _, body := get(t, portal, "/"); body != "<html>themed home</html>" {
		t.Fatalf("themed homepage not served: %q", body)
	}
	if theme.gotPage != themePageSearch {
		t.Errorf("page = %q, want search", theme.gotPage)
	}
	if theme.gotView["submitted"] != false || theme.gotView["query"] != "" {
		t.Errorf("homepage view mismatch: %+v", theme.gotView)
	}
}

func TestThemedPortalKeepsSecurityHeaders(t *testing.T) {
	t.Parallel()

	portal := New(&fakeSource{}, false)
	portal.SetTheme(&fakeTheme{html: "<p>t</p>", ok: true})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	portal.ServeHTTP(rec, req)

	headers := rec.Result().Header
	if got := headers.Get("Content-Type"); got != htmlType {
		t.Errorf("content type = %q", got)
	}
	if headers.Get("X-Content-Type-Options") != "nosniff" ||
		headers.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("security headers missing: %v", headers)
	}
}

func TestThemedPortalSurvivesWriteFailure(t *testing.T) {
	t.Parallel()

	portal := New(&fakeSource{}, false)
	portal.SetTheme(&fakeTheme{html: "<p>t</p>", ok: true})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	portal.ServeHTTP(&failingResponseWriter{}, req)
}

func TestDecliningThemeFallsBackToBuiltinTemplate(t *testing.T) {
	t.Parallel()

	theme := &fakeTheme{ok: false}
	portal := New(&fakeSource{results: richResults()}, false)
	portal.SetTheme(theme)

	_, body := get(t, portal, "/?q=cats")
	if theme.rendered != 1 {
		t.Fatalf("theme consulted %d times, want 1", theme.rendered)
	}
	if !strings.Contains(body, "example.com/a") {
		t.Fatalf("built-in render missing: %q", body)
	}
}

func TestThemeViewCarriesTheDocumentedModel(t *testing.T) {
	t.Parallel()

	theme := &fakeTheme{ok: false}
	portal := New(&fakeSource{results: richResults()}, true)
	portal.SetTheme(theme)
	get(t, portal, "/?q=cats&p=2")

	view := theme.gotView
	assertTopLevelView(t, view)
	assertResultsView(t, view)
	assertPaginationView(t, view)
}

func assertTopLevelView(t *testing.T, view map[string]any) {
	t.Helper()
	if view["query"] != "cats" || view["submitted"] != true || view["newTab"] != true {
		t.Fatalf("top-level view mismatch: %+v", view)
	}
	if view["rssUrl"] != "/yacysearch.rss?query=cats" ||
		view["jsonUrl"] != "/yacysearch.json?query=cats" {
		t.Errorf("format links mismatch: %+v", view)
	}
	if view["elapsed"] == "" || view["error"] != "" || view["dom"] != "" {
		t.Errorf("meta fields mismatch: %+v", view)
	}
	if view["imageVertical"] != false {
		t.Errorf("imageVertical must be false for the all vertical: %+v", view)
	}
	verticals, ok := view["verticals"].([]map[string]any)
	if !ok || len(verticals) != 5 || verticals[0]["label"] != "All" {
		t.Fatalf("verticals mismatch: %+v", view["verticals"])
	}
	if verticals[0]["current"] != true || verticals[1]["url"] != "/?dom=image&q=cats" {
		t.Errorf("vertical entries mismatch: %+v", verticals)
	}
}

func assertResultsView(t *testing.T, view map[string]any) {
	t.Helper()
	results, ok := view["results"].(map[string]any)
	if !ok {
		t.Fatalf("results missing: %+v", view)
	}
	for key, want := range map[string]any{
		"query": "cats", "totalResults": 12, "localCount": 5, "peerCount": 4,
		"webCount": 3, "peersFailed": 1, "recovered": true,
		"didYouMean": "cat", "didYouMeanUrl": "/?q=cat",
	} {
		if results[key] != want {
			t.Errorf("results[%s] = %v, want %v", key, results[key], want)
		}
	}
	assertHitView(t, results)
	assertFacetView(t, results)
}

func assertHitView(t *testing.T, results map[string]any) {
	t.Helper()
	hits, ok := results["results"].([]map[string]any)
	if !ok || len(hits) != 1 {
		t.Fatalf("hits mismatch: %+v", results["results"])
	}
	for key, want := range map[string]any{
		"title": "First <hit>", "url": "https://example.com/a",
		"displayUrl": "example.com/a", "snippet": "plain",
		"snippetHtml": "high <mark>light</mark>", "host": "example.com",
		"date": "2026-07-01", "sizeName": "12 kB", "cachedUrl": "/cache?u=1",
		"provenance": "local", "faviconUrl": "/favicon?host=example.com",
	} {
		if hits[0][key] != want {
			t.Errorf("hit[%s] = %v, want %v", key, hits[0][key], want)
		}
	}
	images, ok := hits[0]["images"].([]map[string]any)
	if !ok || len(images) != 1 || images[0]["proxyUrl"] != "/img?u=1" ||
		images[0]["alt"] != "an image" || images[0]["pageUrl"] != "https://example.com/a" {
		t.Errorf("images mismatch: %+v", hits[0]["images"])
	}
}

func assertFacetView(t *testing.T, results map[string]any) {
	t.Helper()
	facets, ok := results["facets"].([]map[string]any)
	if !ok || len(facets) != 1 || facets[0]["title"] != "Hosts" {
		t.Fatalf("facets mismatch: %+v", results["facets"])
	}
	items, ok := facets[0]["items"].([]map[string]any)
	if !ok || len(items) != 1 || items[0]["label"] != "example.com" ||
		items[0]["count"] != 5 || items[0]["url"] != "/?q=cats+site%3Aexample.com" {
		t.Errorf("facet items mismatch: %+v", facets[0]["items"])
	}
}

func assertPaginationView(t *testing.T, view map[string]any) {
	t.Helper()
	pagination, ok := view["pagination"].(map[string]any)
	if !ok || pagination["page"] != 2 || pagination["hasPrev"] != true {
		t.Fatalf("pagination mismatch: %+v", view["pagination"])
	}
	if pagination["show"] != true {
		t.Errorf("pagination.show must derive from prev/next/pages: %+v", pagination)
	}
	pages, ok := pagination["pages"].([]map[string]any)
	if !ok || len(pages) != 2 || pages[1]["current"] != true ||
		pages[0]["number"] != 1 || pages[0]["url"] != "/?p=1&q=cats" {
		t.Errorf("page links mismatch: %+v", pagination["pages"])
	}
}

func TestThemeViewCarriesSearchFailure(t *testing.T) {
	t.Parallel()

	theme := &fakeTheme{ok: false}
	portal := New(&fakeSource{err: errFailingSource}, false)
	portal.SetTheme(theme)
	get(t, portal, "/?q=cats")

	if theme.gotPage != themePageResults {
		t.Errorf("failure page = %q, want results", theme.gotPage)
	}
	if theme.gotView["error"] != "Search is temporarily unavailable." ||
		theme.gotView["submitted"] != false {
		t.Errorf("failure view mismatch: %+v", theme.gotView)
	}
}
