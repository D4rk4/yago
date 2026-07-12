package yacysearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

type fakeSearch struct {
	response searchcore.Response
	err      error
	got      searchcore.Request
}

func (s *fakeSearch) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.got = req
	if s.err != nil {
		return searchcore.Response{}, s.err
	}
	s.response.Request = req

	return s.response, nil
}

func TestJSONEndpointReturnsYaCySearchShape(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:      "Result",
			URL:        "https://example.org/doc",
			Snippet:    "Result snippet",
			Score:      7,
			Host:       "example.org",
			Path:       "/doc",
			File:       "doc",
			URLHash:    "AAAAAAAAAAAA",
			Size:       12,
			Date:       "20260101",
			Source:     searchcore.SourceLocal,
			DisplayURL: "example.org/doc",
		}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.json?query=golang+site:example.org&maximumRecords=50&startRecord=0&resource=local&contentdom=text&verify=false",
		nil,
	)
	jsonEndpoint{search: search}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	var got jsonResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	channel := got.Channels[0]
	if channel.TotalResults != "1" ||
		channel.ItemsPerPage != "10" ||
		channel.Link != "http://node.test/yacysearch.html?query=golang+site%3Aexample.org&amp;resource=local&amp;contentdom=text" ||
		len(channel.Items) != 1 {
		t.Fatalf("channel = %#v", channel)
	}
	if item := channel.Items[0]; item.Title != "Result" ||
		item.Size != "12" ||
		item.SizeName != "12 bytes" ||
		item.Ranking != "7" {
		t.Fatalf("item = %#v", item)
	}
	if search.got.SiteHost != "example.org" ||
		search.got.Limit != publicSearchLimitCap ||
		search.got.Source != searchcore.SourceLocal {
		t.Fatalf("request = %#v", search.got)
	}
}

func TestJSONEndpointRejectsBadRequests(t *testing.T) {
	cases := []string{
		"?maximumRecords=bad",
		"?startRecord=bad",
		"?resource=remote",
		"?contentdom=book",
		"?verify=online",
		"?urlmaskfilter=[",
	}
	for _, query := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			yagoproto.PathYaCySearchJSON+query,
			nil,
		)
		jsonEndpoint{search: &fakeSearch{}}.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", query, rec.Code)
		}
	}
}

func TestJSONEndpointRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathYaCySearchJSON,
		nil,
	)
	jsonEndpoint{search: &fakeSearch{}}.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status = %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestJSONEndpointReturnsSearchErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathYaCySearchJSON,
		nil,
	)
	jsonEndpoint{search: &fakeSearch{err: errors.New("boom")}}.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestMountJSONRegistersEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	MountJSON(mux, &fakeSearch{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathYaCySearchJSON,
		nil,
	)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestSearchRequestFromValuesMergesOperatorsAndParams(t *testing.T) {
	values := url.Values{
		yagoproto.FieldQuery: {
			"golang LANGUAGE:de inurl:docs tld:org filetype:pdf NEAR /date -java",
		},
		yagoproto.FieldLanguage:         {"en"},
		yagoproto.FieldSiteHost:         {"example.org"},
		yagoproto.FieldMaximumRecords:   {"3"},
		yagoproto.FieldStartRecord:      {"2"},
		yagoproto.FieldResource:         {"global"},
		yagoproto.FieldContentDom:       {"all"},
		yagoproto.FieldFileType:         {"html"},
		yagoproto.FieldURLMaskFilter:    {".*"},
		yagoproto.FieldPreferMaskFilter: {"docs"},
		yagoproto.FieldVerify:           {"cacheonly"},
		yagoproto.FieldNavigation:       {"none"},
	}

	got, err := searchRequestFromValues(values)
	if err != nil {
		t.Fatalf("searchRequestFromValues: %v", err)
	}
	if got.Language != "en" ||
		got.SiteHost != "example.org" ||
		got.FileType != "html" ||
		got.Source != searchcore.SourceGlobal ||
		got.ContentDomain != searchcore.ContentDomainAll ||
		got.Verify != searchcore.VerifyCacheOnly ||
		got.Limit != 3 ||
		got.Offset != 2 ||
		got.Navigation != "none" ||
		got.WithFacets ||
		!got.SortByDate ||
		!got.Near {
		t.Fatalf("request = %#v", got)
	}
}

func TestResponseJSONUsesFallbackHost(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Host = ""
	got := responseJSON(req, searchcore.Response{Request: searchcore.Request{Limit: 10}})
	if got.Channels[0].Link != "http://127.0.0.1/yacysearch.html?query=&amp;resource=&amp;contentdom=" {
		t.Fatalf("link = %q", got.Channels[0].Link)
	}
}

func TestResponseJSONUsesTLSBaseURL(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://node.test/", nil)
	req.TLS = &tls.ConnectionState{}
	got := responseJSON(req, searchcore.Response{Request: searchcore.Request{
		Query:         "secure",
		Source:        searchcore.SourceLocal,
		ContentDomain: searchcore.ContentDomainText,
		Limit:         10,
	}})

	if got.Channels[0].Image.URL != "https://node.test/env/grafics/yacy.png" {
		t.Fatalf("image url = %q", got.Channels[0].Image.URL)
	}
}
