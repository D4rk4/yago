package publicportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type imageSource struct{ gotDom string }

func (s *imageSource) Search(
	_ context.Context,
	_ string,
	dom string,
	_, _ int,
) (SearchResults, error) {
	s.gotDom = dom

	return SearchResults{
		Query:        "go",
		TotalResults: 1,
		Results: []SearchResult{{
			Title: "Pictured",
			URL:   "https://a.example/p.html",
			Images: []ResultImage{{
				ProxyURL: "/imgproxy?u=https%3A%2F%2Fa.example%2Fshot.png",
				Alt:      "Shot",
				PageURL:  "https://a.example/p.html",
			}},
		}},
	}, nil
}

func TestPortalImageVerticalRendersGridAndTabs(t *testing.T) {
	source := &imageSource{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go&dom=image", nil)
	New(source, false).ServeHTTP(rec, req)

	if source.gotDom != "image" {
		t.Fatalf("dom = %q, want image forwarded to the source", source.gotDom)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<nav class="tabs"`, `<span aria-current="page">Images</span>`, ">All</a>",
		`<ul class="imggrid">`, `loading="lazy"`, `class="lightbox" id="img-0-0"`,
		`alt="Shot"`, ">Pictured</a>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("image vertical missing %q", want)
		}
	}
}

func TestPortalRejectsUnknownDom(t *testing.T) {
	source := &imageSource{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go&dom=bogus", nil)
	New(source, false).ServeHTTP(rec, req)
	if source.gotDom != "" {
		t.Fatalf("dom = %q, want bogus vertical rejected", source.gotDom)
	}
}
