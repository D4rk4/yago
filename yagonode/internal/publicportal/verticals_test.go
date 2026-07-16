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

type imagePaginationSource struct{}

func (imagePaginationSource) Search(
	context.Context,
	string,
	string,
	int,
	int,
) (SearchResults, error) {
	return SearchResults{
		Query:        "go",
		TotalResults: 100,
		Availability: SearchAvailability{Materialized: 21},
		Results: []SearchResult{{
			Title: "Pictured",
			URL:   "https://a.example/p.html",
			Images: []ResultImage{{
				ProxyURL: "/imgproxy?u=shot",
				Alt:      "Shot",
				PageURL:  "https://a.example/p.html",
			}},
		}},
	}, nil
}

func TestPortalImageVerticalPaginationKeepsDomain(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/?q=go&dom=image&p=2",
		nil,
	)
	New(imagePaginationSource{}, false).ServeHTTP(recorder, request)

	body := recorder.Body.String()
	for _, link := range []string{
		`rel="prev" href="/?dom=image&amp;p=1&amp;q=go"`,
		`rel="next" href="/?dom=image&amp;p=3&amp;q=go"`,
	} {
		if !strings.Contains(body, link) {
			t.Fatalf("image pagination missing %q", link)
		}
	}
}
