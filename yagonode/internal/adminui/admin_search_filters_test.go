package adminui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type adminSearchFilterRecorder struct {
	request SearchQuery
}

func (recorder *adminSearchFilterRecorder) Search(
	_ context.Context,
	request SearchQuery,
) (SearchResults, error) {
	recorder.request = request

	return SearchResults{
		Query:        request.Query,
		Global:       request.Global,
		TotalResults: 100,
		Results:      []SearchResult{{Title: "hit", URL: "https://docs.example.org/hit"}},
	}, nil
}

func TestConsoleSearchCarriesTypedFiltersThroughPaging(t *testing.T) {
	t.Parallel()

	recorder := &adminSearchFilterRecorder{}
	page := do(
		t,
		New(Options{Search: recorder}),
		"/admin/search?q=bounded&scope=local&p=2&contentdom=image&language=ru&sitehost=docs.example.org",
	)
	if page.status != http.StatusOK {
		t.Fatalf("status = %d", page.status)
	}
	if recorder.request.Filters != (SearchFilters{
		ContentDomain: "image",
		Language:      "ru",
		SiteHost:      "docs.example.org",
	}) {
		t.Fatalf("filters = %+v", recorder.request.Filters)
	}
	for _, fragment := range []string{
		`value="image" selected`,
		`name="language" value="ru"`,
		`name="sitehost" value="docs.example.org"`,
		"contentdom=image",
		"language=ru",
		"sitehost=docs.example.org",
		"p=1",
		"p=3",
	} {
		if !strings.Contains(page.body, fragment) {
			t.Fatalf("filtered search page missing %q", fragment)
		}
	}
}

func TestConsoleSearchCanonicalRedirectPreservesTypedFilters(t *testing.T) {
	t.Parallel()

	recorder := &adminSearchFilterRecorder{}
	page := do(
		t,
		New(Options{Search: recorder}),
		"/admin/search?q=bounded&scope=global&p=99999&contentdom=audio&language=fi&sitehost=radio.example",
	)
	if page.status != http.StatusSeeOther {
		t.Fatalf("status = %d", page.status)
	}
	location, err := url.Parse(page.header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	values := location.Query()
	if location.Path != searchPath || values.Get("p") != "5" ||
		values.Get("q") != "bounded" || values.Get("scope") != "global" ||
		values.Get("contentdom") != "audio" || values.Get("language") != "fi" ||
		values.Get("sitehost") != "radio.example" {
		t.Fatalf("redirect = %q", location.String())
	}
}
