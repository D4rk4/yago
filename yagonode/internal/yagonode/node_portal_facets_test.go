package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPortalSourceRendersFacetSidebar(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 2,
		Results: []searchcore.Result{
			{Title: "Go", URL: "http://a.example/1", Host: "a.example", Language: "en"},
			{Title: "Search", URL: "http://a.example/2", Host: "a.example", Language: "en"},
		},
	}}

	results, err := newPortalSource(searcher).Search(context.Background(), "go", "", 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if searcher.gotRequest.WithFacets {
		t.Fatal("portal requested a complete corpus facet scan")
	}
	if len(results.Facets) != 2 || results.Facets[0].Title != "Host" {
		t.Fatalf("facets = %+v", results.Facets)
	}
	host := results.Facets[0].Items[0]
	if host.URL != "/?q=go+site%3Aa.example" || host.Count != 2 {
		t.Fatalf("host item = %+v, want site: operator link", host)
	}
	if language := results.Facets[1].Items[0]; language.URL != "/?q=go+language%3Aen" {
		t.Fatalf("language item = %+v, want language: operator link", language)
	}
}
