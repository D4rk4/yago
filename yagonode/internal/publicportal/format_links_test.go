package publicportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type cachedSource struct{}

func (cachedSource) Search(
	context.Context,
	string,
	string,
	int,
	int,
) (SearchResults, error) {
	return SearchResults{
		Query:        "go",
		TotalResults: 2,
		LocalCount:   1,
		PeerCount:    1,
		Results: []SearchResult{
			{
				Title:      "Local",
				URL:        "https://a.example/x",
				CachedURL:  "/cached?u=https%3A%2F%2Fa.example%2Fx",
				Provenance: "local",
				FaviconURL: "/favicon?host=a.example",
				Date:       "Wed, 20 May 2026",
			},
			{
				Title:      "Peer",
				URL:        "https://b.example/y",
				Provenance: "peer",
			},
		},
	}, nil
}

func TestPortalLinksFormatsAndCachedCopy(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/?q=go", nil,
	)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`rel="alternate" type="application/rss+xml"`,
		`href="/yacysearch.rss?query=go"`,
		`href="/yacysearch.json?query=go"`,
		">RSS</a>", ">JSON</a>", `href="/opensearch.xml">OpenSearch</a>`,
		`href="/cached?u=https%3A%2F%2Fa.example%2Fx">cached</a>`,
		`<span class="prov prov-local">local</span>`,
		`<span class="prov prov-peer">peer</span>`,
		`<img class="fav" src="/favicon?host=a.example"`,
		"Wed, 20 May 2026",
		"Showing 1–2 on this page: 1 from this node · 1 from peers · 0 from DDGS.",
		"Searches fan out to peers in the YaCy network",
		`class="prov prov-ddgs">[ddgs]</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("portal missing %q: %s", want, body)
		}
	}
}

func TestPortalHidesFormatLinksWithoutQuery(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "yacysearch.rss") {
		t.Fatal("format links rendered without a query")
	}
}
