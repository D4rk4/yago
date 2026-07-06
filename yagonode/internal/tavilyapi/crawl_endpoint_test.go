package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sitePages is a scripted site graph for the crawl walker.
type sitePages map[string]CrawledPage

func (s sitePages) FetchPage(_ context.Context, url string) (CrawledPage, error) {
	page, ok := s[url]
	if !ok {
		return CrawledPage{}, errors.New("fetch failed")
	}

	return page, nil
}

func testSite() sitePages {
	return sitePages{
		"https://site.example/": {
			Title: "Home",
			Text:  "Welcome home page text.",
			Links: []string{
				"https://site.example/docs",
				"https://site.example/blog",
				"https://site.example/private/admin",
				"https://other.example/away",
				"https://www.site.example/alias",
			},
		},
		"https://site.example/docs": {
			Title: "Docs", Text: "Documentation text.",
			Links: []string{"https://site.example/docs/deep"},
		},
		"https://site.example/blog":            {Title: "Blog", Text: "Blog text."},
		"https://site.example/private/admin":   {Title: "Admin", Text: "Secret."},
		"https://www.site.example/alias":       {Title: "Alias", Text: "Alias text."},
		"https://site.example/docs/deep":       {Title: "Deep", Text: "Deep text."},
		"https://other.example/away":           {Title: "Away", Text: "External text."},
		"https://single.example/preauthorized": {Title: "One", Text: "Single."},
	}
}

func doCrawl(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	MountCrawl(mux, SearchAccessPolicy{}, testSite())
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, path, strings.NewReader(body),
	)
	mux.ServeHTTP(rec, req)

	return rec
}

func TestCrawlWalksBoundedSameSite(t *testing.T) {
	rec := doCrawl(t, PathCrawl, `{
		"url":"https://site.example/",
		"max_depth":1,
		"exclude_paths":["^/private"],
		"format":"markdown",
		"include_favicon":true
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp CrawlResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BaseURL != "https://site.example/" || resp.RequestID == "" {
		t.Fatalf("envelope = %+v", resp)
	}
	got := map[string]CrawlResult{}
	for _, result := range resp.Results {
		got[result.URL] = result
	}
	for _, want := range []string{
		"https://site.example/", "https://site.example/docs",
		"https://site.example/blog", "https://www.site.example/alias",
	} {
		if _, ok := got[want]; !ok {
			t.Fatalf("crawl missing %s: %+v", want, resp.Results)
		}
	}
	if _, ok := got["https://site.example/private/admin"]; ok {
		t.Fatal("excluded path crawled")
	}
	if _, ok := got["https://other.example/away"]; ok {
		t.Fatal("external domain crawled without allow_external")
	}
	if _, ok := got["https://site.example/docs/deep"]; ok {
		t.Fatal("depth-2 page crawled at max_depth 1")
	}
	home := got["https://site.example/"]
	if !strings.HasPrefix(home.RawContent, "# Home") || home.Favicon == "" {
		t.Fatalf("home result = %+v", home)
	}
}

func TestCrawlOptionsAndErrors(t *testing.T) {
	// allow_external follows off-site links.
	rec := doCrawl(t, PathCrawl, `{"url":"https://site.example/","allow_external":true}`)
	var resp CrawlResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	found := false
	for _, result := range resp.Results {
		if result.URL == "https://other.example/away" {
			found = true
		}
	}
	if !found {
		t.Fatal("allow_external must follow external links")
	}

	// select_paths keeps only matching links; select_domains scopes hosts.
	rec = doCrawl(t, PathCrawl, `{"url":"https://site.example/","select_paths":["^/docs"]}`)
	resp = CrawlResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 2 {
		t.Fatalf("select_paths results = %+v", resp.Results)
	}

	for body, fragment := range map[string]string{
		`{"url":"ftp://x"}`:                                    "url must be absolute",
		`{"url":"https://site.example/","max_depth":9}`:        "max_depth",
		`{"url":"https://site.example/","max_breadth":0}`:      "max_breadth",
		`{"url":"https://site.example/","limit":0}`:            "limit",
		`{"url":"https://site.example/","select_paths":["("]}`: "select_paths",
		`not json`: "invalid JSON",
	} {
		rec := doCrawl(t, PathCrawl, body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), fragment) {
			t.Fatalf("body %q = %d %s", body, rec.Code, rec.Body.String())
		}
	}

	// Method and availability guards.
	mux := http.NewServeMux()
	MountCrawl(mux, SearchAccessPolicy{}, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, PathCrawl, nil,
	))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, PathCrawl, strings.NewReader(`{"url":"https://x.example/"}`),
	))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil fetcher = %d", rec.Code)
	}

	// Auth: a bearer-token policy rejects anonymous crawls.
	authed := http.NewServeMux()
	MountCrawl(authed, SearchAccessPolicy{BearerToken: "secret"}, testSite())
	rec = httptest.NewRecorder()
	authed.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathCrawl,
		strings.NewReader(`{"url":"https://site.example/"}`),
	))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous crawl = %d", rec.Code)
	}
}

func TestMapReturnsDiscoveredURLs(t *testing.T) {
	rec := doCrawl(t, PathMap, `{"url":"https://site.example/","max_depth":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp MapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	joined := strings.Join(resp.Results, " ")
	for _, want := range []string{
		"https://site.example/", "https://site.example/docs", "https://site.example/docs/deep",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("map missing %s: %v", want, resp.Results)
		}
	}
	// Map results carry no content field at all.
	if strings.Contains(rec.Body.String(), "raw_content") {
		t.Fatal("map response must not carry content")
	}
}

func TestCrawlWalkEdgeBranches(t *testing.T) {
	endpoint := crawlEndpoint{fetcher: testSite(), now: timeFilterClock}

	// A canceled context stops the walk after validation.
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	entries, _, err := endpoint.walk(canceled, CrawlRequest{URL: "https://site.example/"})
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled walk = %v %d", err, len(entries))
	}

	// A fetch failure skips the page and continues.
	entries, _, err = endpoint.walk(t.Context(), CrawlRequest{URL: "https://missing.example/"})
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed fetch walk = %v %d", err, len(entries))
	}

	// The breadth cap bounds enqueued links per page.
	one := 1
	entries, _, err = endpoint.walk(t.Context(), CrawlRequest{
		URL: "https://site.example/", MaxBreadth: &one,
	})
	if err != nil || len(entries) != 2 {
		t.Fatalf("breadth-capped walk = %v %d", err, len(entries))
	}

	// The limit bounds total pages.
	limit := 1
	entries, _, err = endpoint.walk(t.Context(), CrawlRequest{
		URL: "https://site.example/", Limit: &limit,
	})
	if err != nil || len(entries) != 1 {
		t.Fatalf("limited walk = %v %d", err, len(entries))
	}

	// select_domains scopes hosts; bad patterns in the remaining fields fail.
	entries, _, err = endpoint.walk(t.Context(), CrawlRequest{
		URL: "https://site.example/", AllowExternal: true,
		SelectDomains: []string{"^other\\."},
	})
	if err != nil {
		t.Fatalf("select_domains walk: %v", err)
	}
	for _, entry := range entries[1:] {
		if !strings.Contains(entry.url, "other.example") {
			t.Fatalf("select_domains leak: %v", entry.url)
		}
	}
	excluded, _, err := endpoint.walk(t.Context(), CrawlRequest{
		URL: "https://site.example/", ExcludeDomains: []string{"^www\\."},
	})
	if err != nil {
		t.Fatalf("exclude_domains walk: %v", err)
	}
	for _, entry := range excluded {
		if strings.Contains(entry.url, "www.site.example") {
			t.Fatal("exclude_domains leak")
		}
	}
	for _, req := range []CrawlRequest{
		{URL: "https://site.example/", ExcludePaths: []string{"("}},
		{URL: "https://site.example/", SelectDomains: []string{"("}},
		{URL: "https://site.example/", ExcludeDomains: []string{"("}},
	} {
		if _, _, err := endpoint.walk(t.Context(), req); err == nil {
			t.Fatal("bad pattern must fail")
		}
	}

	// Unparsable discovered links drop.
	if (crawlFilters{baseHost: "x"}).allows("http://bad host/") {
		t.Fatal("unparsable link allowed")
	}
}
