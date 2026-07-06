package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPortalSourceRendersFacetSidebar(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 1,
		Results:      []searchcore.Result{{Title: "Go", URL: "http://a/1"}},
		Facets: []searchcore.FacetGroup{
			{Name: "host", Terms: []searchcore.FacetTerm{{Term: "a.example", Count: 3}}},
			{Name: "month", Terms: []searchcore.FacetTerm{{Term: "2026-05", Count: 2}}},
		},
	}}

	results, err := newPortalSource(searcher).Search(context.Background(), "go", "", 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !searcher.gotRequest.WithFacets {
		t.Fatal("portal must request facets")
	}
	if len(results.Facets) != 2 || results.Facets[0].Title != "Host" {
		t.Fatalf("facets = %+v", results.Facets)
	}
	host := results.Facets[0].Items[0]
	if host.URL != "/?q=go+site%3Aa.example" || host.Count != 3 {
		t.Fatalf("host item = %+v, want site: operator link", host)
	}
	if month := results.Facets[1].Items[0]; month.URL != "" {
		t.Fatalf("month item = %+v, want plain count without a link", month)
	}
}
