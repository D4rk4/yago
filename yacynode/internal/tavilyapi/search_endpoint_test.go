package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/searchcore"
)

type fakeSearcher struct {
	response searchcore.Response
	err      error
	got      searchcore.Request
	calls    int
}

func (s *fakeSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.calls++
	s.got = req
	if s.err != nil {
		return searchcore.Response{}, s.err
	}

	return s.response, nil
}

type fakeDocuments struct {
	rows map[string]documentstore.Document
	err  error
	got  string
}

func (d *fakeDocuments) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	d.got = normalizedURL
	if d.err != nil {
		return documentstore.Document{}, false, d.err
	}
	doc, found := d.rows[normalizedURL]

	return doc, found, nil
}

func (d *fakeDocuments) Count(context.Context) (int, error) { return len(d.rows), nil }

func TestSearchEndpointReturnsTavilyShape(t *testing.T) {
	endpoint, search, documents := richSearchEndpoint()
	body := `{
		"query":"golang site:ignored.example",
		"search_depth":"advanced",
		"max_results":2,
		"include_answer":"basic",
		"include_raw_content":true,
		"include_domains":["https://example.org"],
		"exclude_domains":["blocked.example"],
		"topic":"general",
		"time_range":"week",
		"safe_search":true
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(body),
	)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}

	assertRichSearchResponse(t, decodeSearchResponse(t, rec), search, documents)
}

func richSearchEndpoint() (searchEndpoint, *fakeSearcher, *fakeDocuments) {
	search := &fakeSearcher{response: searchcore.Response{
		Results: []searchcore.Result{
			{
				Title:   "Metadata title",
				URL:     "https://example.org/doc",
				Snippet: "metadata snippet",
				Score:   9.5,
				Host:    "example.org",
				Date:    "2026-07-02",
			},
			{
				Title:   "Excluded",
				URL:     "https://blocked.example/doc",
				Snippet: "blocked",
				Score:   1,
				Host:    "blocked.example",
			},
		},
	}}
	documents := &fakeDocuments{rows: map[string]documentstore.Document{
		"https://example.org/doc": {
			Title:         "Document title",
			ExtractedText: "Document text with enough local content for an agent response.",
		},
	}}

	return searchEndpoint{
		search:    search,
		documents: documents,
		now:       fixedClock(time.Unix(100, 0), time.Unix(100, int64(250*time.Millisecond))),
	}, search, documents
}

func assertRichSearchResponse(
	t *testing.T,
	got SearchResponse,
	search *fakeSearcher,
	documents *fakeDocuments,
) {
	t.Helper()

	if got.Query != "golang site:ignored.example" ||
		len(got.Results) != 1 ||
		got.Results[0].Title != "Document title" ||
		got.Results[0].Content != "Document text with enough local content for an agent response." ||
		got.Results[0].RawContent == nil ||
		*got.Results[0].RawContent != "Document text with enough local content for an agent response." ||
		got.Results[0].Score != 9.5 ||
		got.Results[0].PublishedDate != "2026-07-02" ||
		got.Results[0].Source != "global" ||
		got.ResponseTime != 0.25 {
		t.Fatalf("response = %#v", got)
	}
	if search.got.Source != searchcore.SourceGlobal ||
		search.got.Limit != 2 ||
		search.got.SiteHost != "example.org" ||
		search.got.ContentDomain != searchcore.ContentDomainText ||
		documents.got != "https://example.org/doc" {
		t.Fatalf("search=%#v doc=%q", search.got, documents.got)
	}
}

func TestSearchEndpointDefaultsToLocalAndMetadataSnippet(t *testing.T) {
	search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{{
		Title:   "Title",
		URL:     "https://sub.example.org/doc",
		Snippet: "metadata snippet",
		Score:   3,
	}}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"golang","include_domains":["example.org"]}`),
	)

	NewSearchEndpoint(search, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeSearchResponse(t, rec)
	if search.got.Source != searchcore.SourceLocal ||
		search.got.Limit != defaultMaxResults ||
		got.Results[0].Content != "metadata snippet" ||
		got.Results[0].RawContent != nil {
		t.Fatalf("request=%#v response=%#v", search.got, got)
	}
}

func TestSearchEndpointFiltersMismatchedIncludeDomains(t *testing.T) {
	search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{{
		Title:   "Title",
		URL:     "https://example.net/doc",
		Snippet: "metadata snippet",
		Score:   3,
		Host:    "example.net",
	}}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"golang","include_domains":["example.org"]}`),
	)

	NewSearchEndpoint(search, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeSearchResponse(t, rec)
	if len(got.Results) != 0 {
		t.Fatalf("results = %#v", got.Results)
	}
}

func TestSearchEndpointUsesTitleWhenContentIsMissing(t *testing.T) {
	search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{{
		Title: "Title",
		URL:   "https://example.org/doc",
	}}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"golang","include_answer":true}`),
	)

	NewSearchEndpoint(search, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeSearchResponse(t, rec)
	if got.Results[0].Content != "Title" {
		t.Fatalf("response = %#v", got)
	}
}

func TestSearchEndpointHandlesEmptyResultLimit(t *testing.T) {
	limit := 0
	search := &fakeSearcher{}
	endpoint := searchEndpoint{
		search: search,
		now:    fixedClock(time.Unix(0, 0), time.Unix(1, 0)),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"golang","max_results":0}`),
	)

	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if search.calls != 0 {
		t.Fatalf("search calls = %d, want 0", search.calls)
	}
	var got SearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Results) != limit || got.ResponseTime != 1 {
		t.Fatalf("response = %#v", got)
	}
}

func TestSearchEndpointRejectsBadRequests(t *testing.T) {
	for _, tc := range badRequestCases() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(
			t.Context(),
			tc.method,
			PathSearch,
			strings.NewReader(tc.body),
		)

		NewSearchEndpoint(&fakeSearcher{}, nil).ServeHTTP(rec, req)

		if rec.Code != tc.code {
			t.Fatalf("%s: status = %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
		if tc.method != http.MethodPost && rec.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("%s: allow = %q", tc.name, rec.Header().Get("Allow"))
		}
	}
}

type badRequestCase struct {
	name   string
	method string
	body   string
	code   int
}

func badRequestCases() []badRequestCase {
	return []badRequestCase{
		{name: "method", method: http.MethodGet, body: `{}`, code: http.StatusMethodNotAllowed},
		{name: "json", method: http.MethodPost, body: `{`, code: http.StatusBadRequest},
		{
			name:   "unknown",
			method: http.MethodPost,
			body:   `{"query":"go","unknown":true}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "query",
			method: http.MethodPost,
			body:   `{"query":" "}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "depth",
			method: http.MethodPost,
			body:   `{"query":"go","search_depth":"deep"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "max",
			method: http.MethodPost,
			body:   `{"query":"go","max_results":21}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "topic",
			method: http.MethodPost,
			body:   `{"query":"go","topic":"sports"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "time",
			method: http.MethodPost,
			body:   `{"query":"go","time_range":"hour"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "domain",
			method: http.MethodPost,
			body:   `{"query":"go","include_domains":["bad/path"]}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "answer",
			method: http.MethodPost,
			body:   `{"query":"go","include_answer":"full"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "answer type",
			method: http.MethodPost,
			body:   `{"query":"go","include_answer":7}`,
			code:   http.StatusBadRequest,
		},
	}
}

func TestSearchEndpointReturnsSearchAndDocumentErrors(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			PathSearch,
			strings.NewReader(`{"query":"go"}`),
		)

		NewSearchEndpoint(&fakeSearcher{err: errors.New("boom")}, nil).ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("document", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			PathSearch,
			strings.NewReader(`{"query":"go"}`),
		)
		search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{{
			URL: "https://example.org/",
		}}}}
		docs := &fakeDocuments{err: errors.New("read failed")}

		NewSearchEndpoint(search, docs).ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestSearchEndpointMountsRoute(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, &fakeSearcher{}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"go"}`),
	)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHelpersCoverAliasesAndBounds(t *testing.T) {
	for _, depth := range []string{"", "basic", "fast", "ultra-fast"} {
		source, err := sourceForDepth(depth)
		if err != nil || source != searchcore.SourceLocal {
			t.Fatalf("sourceForDepth(%q) = %q, %v", depth, source, err)
		}
	}
	for _, topic := range []string{"", "news", "finance"} {
		if err := validateTopic(topic); err != nil {
			t.Fatalf("validateTopic(%q): %v", topic, err)
		}
	}
	for _, span := range []string{"", "day", "d", "month", "m", "year", "y"} {
		if err := validateTimeRange(span); err != nil {
			t.Fatalf("validateTimeRange(%q): %v", span, err)
		}
	}
	if !domainMatches("docs.example.org", ".example.org") ||
		domainMatches("example.net", "example.org") ||
		domainMatches("", "example.org") ||
		domainMatches("example.org", "") ||
		normalizeDomain("https://example.org/path") != "" {
		t.Fatal("domain helper mismatch")
	}
	if firstDomain([]string{"", "bad/path"}) != "" ||
		normalizeDomain("") != "" ||
		normalizeDomain("://bad") != "" {
		t.Fatal("invalid domain helper mismatch")
	}
	long := strings.Repeat("x", snippetRuneCap+10)
	if len([]rune(snippet(long))) != snippetRuneCap {
		t.Fatal("snippet was not bounded")
	}
	var mode inclusionMode
	if err := json.Unmarshal([]byte(`false`), &mode); err != nil || mode != "false" {
		t.Fatalf("boolean mode = %q, %v", mode, err)
	}
}

func fixedClock(times ...time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		if index >= len(times) {
			return times[len(times)-1]
		}
		value := times[index]
		index++

		return value
	}
}

func decodeSearchResponse(t *testing.T, rec *httptest.ResponseRecorder) SearchResponse {
	t.Helper()

	var got SearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return got
}
