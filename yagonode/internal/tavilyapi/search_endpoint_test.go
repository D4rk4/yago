package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const searchTestKey = "search-test-key"

// newTestSearchEndpoint builds the endpoint with a static test credential:
// SEC-02 removed the open default, so every test request authenticates.
func newTestSearchEndpoint(
	search searchcore.Searcher,
	documents documentstore.DocumentDirectory,
) http.Handler {
	return NewSearchEndpointWithAccess(
		search,
		documents,
		SearchAccessPolicy{BearerToken: searchTestKey},
	)
}

func TestNewSearchEndpointWiresOpenAccess(t *testing.T) {
	if NewSearchEndpoint(&fakeSearcher{}, &fakeDocuments{}) == nil {
		t.Fatal("NewSearchEndpoint returned a nil handler")
	}
}

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
		"chunks_per_source":2,
		"max_results":2,
		"include_answer":"basic",
		"include_raw_content":"text",
		"include_images":true,
		"include_image_descriptions":true,
		"include_favicon":true,
		"include_domains":["https://example.org"],
		"exclude_domains":["blocked.example"],
		"topic":"general",
		"time_range":"week",
		"start_date":"2026-01-01",
		"end_date":"2026-07-02",
		"country":"united states",
		"auto_parameters":true,
		"include_usage":true,
		"safe_search":true
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(body),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	req.Header.Set(requestIDHeader, "request-123")
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}

	assertRichSearchResponse(t, decodeSearchResponse(t, rec), search, documents)
}

func TestSearchEndpointSafeSearchImagesRequireGeneralEvidence(t *testing.T) {
	tests := []struct {
		name       string
		rating     searchcore.SafetyRating
		wantImages int
	}{
		{name: "unknown", rating: searchcore.SafetyUnknown},
		{name: "general", rating: searchcore.SafetyGeneral, wantImages: 1},
		{name: "explicit", rating: searchcore.SafetyExplicit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resultURL := "https://example.org/document"
			search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{{
				Title: "Title", URL: resultURL, Snippet: "Content", SafetyRating: test.rating,
			}}}}
			documents := &fakeDocuments{rows: map[string]documentstore.Document{
				resultURL: {Images: []documentstore.ImageMetadata{{
					URL: "https://example.org/image.png", AltText: "Image",
				}}},
			}}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				PathSearch,
				strings.NewReader(`{"query":"topic","safe_search":true,"include_images":true}`),
			)
			req.Header.Set("Authorization", "Bearer "+searchTestKey)
			newTestSearchEndpoint(search, documents).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			got := decodeSearchResponse(t, rec)
			if len(got.Results) != 1 || len(got.Results[0].Images) != test.wantImages {
				t.Fatalf("results = %#v, want %d images", got.Results, test.wantImages)
			}
			encodedImages, err := json.Marshal(got.Images)
			if err != nil {
				t.Fatalf("marshal images: %v", err)
			}
			var topImages []string
			if err := json.Unmarshal(encodedImages, &topImages); err != nil ||
				len(topImages) != test.wantImages {
				t.Fatalf("top-level images = %#v, want %d (%v)", got.Images, test.wantImages, err)
			}
		})
	}
}

func richSearchEndpoint() (searchEndpoint, *fakeSearcher, *fakeDocuments) {
	search := &fakeSearcher{response: searchcore.Response{
		Results: []searchcore.Result{
			{
				Title:        "Metadata title",
				URL:          "https://example.org/doc",
				Snippet:      "metadata snippet",
				Score:        9.5,
				Host:         "example.org",
				Date:         "2026-07-02",
				SafetyRating: searchcore.SafetyGeneral,
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
			Images: []documentstore.ImageMetadata{{
				URL:     "https://example.org/image.png",
				AltText: "Document image",
			}},
		},
	}}

	return searchEndpoint{
		access:    SearchAccessPolicy{BearerToken: searchTestKey},
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
		got.Results[0].Favicon != "https://example.org/favicon.ico" ||
		got.Results[0].Source != "global" ||
		got.ResponseTime != 0.25 ||
		got.RequestID != "request-123" ||
		got.Answer == nil ||
		*got.Answer != "" ||
		got.FollowUpQuestions != nil ||
		len(got.Results[0].Images) != 1 ||
		got.Results[0].Images[0] != "https://example.org/image.png" ||
		got.Usage == nil ||
		got.Usage.Credits != 0 ||
		got.AutoParameters["topic"] != "general" ||
		got.AutoParameters["search_depth"] != "advanced" ||
		got.AutoParameters["source"] != "global" {
		t.Fatalf("response = %#v", got)
	}
	// The images field arrives as []SearchImage in-process and as decoded JSON
	// after an HTTP round-trip; normalize through JSON before asserting.
	raw, err := json.Marshal(got.Images)
	if err != nil {
		t.Fatalf("marshal images: %v", err)
	}
	var images []SearchImage
	if err := json.Unmarshal(raw, &images); err != nil || len(images) != 1 ||
		images[0].URL != "https://example.org/image.png" ||
		images[0].Description != "Document image" {
		t.Fatalf("images = %#v, want one described image object (%v)", got.Images, err)
	}
	if search.got.Source != searchcore.SourceGlobal ||
		search.got.Limit != 2 ||
		!search.got.AllowWebFallback ||
		!search.got.SafeSearch ||
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
	req.Header.Set("Authorization", "Bearer "+searchTestKey)

	newTestSearchEndpoint(search, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeSearchResponse(t, rec)
	if search.got.Source != searchcore.SourceLocal ||
		search.got.Limit != defaultMaxResults ||
		!search.got.AllowWebFallback ||
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
	req.Header.Set("Authorization", "Bearer "+searchTestKey)

	newTestSearchEndpoint(search, nil).ServeHTTP(rec, req)

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
	req.Header.Set("Authorization", "Bearer "+searchTestKey)

	newTestSearchEndpoint(search, nil).ServeHTTP(rec, req)

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
		access: SearchAccessPolicy{BearerToken: searchTestKey},
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
	req.Header.Set("Authorization", "Bearer "+searchTestKey)

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

func TestSearchEndpointIgnoresUnknownFields(t *testing.T) {
	search := &fakeSearcher{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"go","future_option":true}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)

	newTestSearchEndpoint(search, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if search.calls != 1 {
		t.Fatalf("search calls = %d", search.calls)
	}
}

func TestSearchEndpointRequiresConfiguredBearerToken(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authorization string
		code          int
		calls         int
	}{
		{name: "missing", code: http.StatusUnauthorized},
		{name: "basic", authorization: "Basic secret", code: http.StatusUnauthorized},
		{name: "wrong", authorization: "Bearer wrong", code: http.StatusUnauthorized},
		{name: "malformed", authorization: "Bearer secret extra", code: http.StatusUnauthorized},
		{name: "valid", authorization: "bearer secret", code: http.StatusOK, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			search := &fakeSearcher{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				PathSearch,
				strings.NewReader(`{"query":"go"}`),
			)
			req.Header.Set("Authorization", "Bearer "+searchTestKey)
			req.Header.Set(requestIDHeader, tc.name)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}

			NewSearchEndpointWithAccess(
				search,
				nil,
				SearchAccessPolicy{BearerToken: "secret"},
			).ServeHTTP(rec, req)

			if rec.Code != tc.code {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if search.calls != tc.calls {
				t.Fatalf("search calls = %d", search.calls)
			}
			if tc.code == http.StatusUnauthorized {
				assertUnauthorizedResponse(t, rec, tc.name)
			}
		})
	}
}

func assertUnauthorizedResponse(
	t *testing.T,
	rec *httptest.ResponseRecorder,
	requestID string,
) {
	t.Helper()

	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("www-authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if got.Error.Code != "unauthorized" ||
		got.Error.Message != "missing or invalid bearer token" ||
		got.RequestID != requestID {
		t.Fatalf("error response = %#v", got)
	}
}

func TestSearchEndpointFiltersExactMatches(t *testing.T) {
	search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{
		{
			Title:   "Matched",
			URL:     "https://example.org/matched",
			Snippet: "John Smith founded the company",
		},
		{
			Title:   "Missed",
			URL:     "https://example.org/missed",
			Snippet: "John Smyth founded the company",
		},
	}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"\"John Smith\" company","exact_match":true}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)

	newTestSearchEndpoint(search, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeSearchResponse(t, rec)
	if len(got.Results) != 1 || got.Results[0].URL != "https://example.org/matched" {
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
		req.Header.Set("Authorization", "Bearer "+searchTestKey)

		newTestSearchEndpoint(&fakeSearcher{}, nil).ServeHTTP(rec, req)

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
	out := badRequestProtocolCases()
	out = append(out, badRequestOptionCases()...)
	out = append(out, badRequestModeCases()...)

	return out
}

func badRequestProtocolCases() []badRequestCase {
	return []badRequestCase{
		{name: "method", method: http.MethodGet, body: `{}`, code: http.StatusMethodNotAllowed},
		{name: "json", method: http.MethodPost, body: `{`, code: http.StatusBadRequest},
		{
			name:   "query",
			method: http.MethodPost,
			body:   `{"query":" "}`,
			code:   http.StatusBadRequest,
		},
	}
}

func badRequestOptionCases() []badRequestCase {
	return []badRequestCase{
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
			name:   "chunks",
			method: http.MethodPost,
			body:   `{"query":"go","chunks_per_source":4}`,
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
			name:   "date",
			method: http.MethodPost,
			body:   `{"query":"go","start_date":"2026-7-2"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "end date",
			method: http.MethodPost,
			body:   `{"query":"go","end_date":"2026-7-2"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "date order",
			method: http.MethodPost,
			body:   `{"query":"go","start_date":"2026-07-03","end_date":"2026-07-02"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "country",
			method: http.MethodPost,
			body:   "{\"query\":\"go\",\"country\":\"bad\\ncountry\"}",
			code:   http.StatusBadRequest,
		},
		{
			name:   "domain",
			method: http.MethodPost,
			body:   `{"query":"go","include_domains":["bad/path?query"]}`,
			code:   http.StatusBadRequest,
		},
	}
}

func badRequestModeCases() []badRequestCase {
	return []badRequestCase{
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
		{
			name:   "raw",
			method: http.MethodPost,
			body:   `{"query":"go","include_raw_content":"full"}`,
			code:   http.StatusBadRequest,
		},
		{
			name:   "raw type",
			method: http.MethodPost,
			body:   `{"query":"go","include_raw_content":7}`,
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
		req.Header.Set("Authorization", "Bearer "+searchTestKey)

		newTestSearchEndpoint(&fakeSearcher{err: errors.New("boom")}, nil).ServeHTTP(rec, req)

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
		req.Header.Set("Authorization", "Bearer "+searchTestKey)
		search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{{
			URL: "https://example.org/",
		}}}}
		docs := &fakeDocuments{err: errors.New("read failed")}

		newTestSearchEndpoint(search, docs).ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestSearchEndpointMountsRoute(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, &fakeSearcher{}, nil, SearchAccessPolicy{BearerToken: searchTestKey}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"go"}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchDepthAndValidationHelpers(t *testing.T) {
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
}

func TestDomainHelpers(t *testing.T) {
	if !domainMatches("docs.example.org", ".example.org") ||
		domainMatches("example.net", "example.org") ||
		domainMatches("", "example.org") ||
		domainMatches("example.org", "") ||
		normalizeDomain("https://example.org/path") != "example.org" ||
		normalizeDomain("linkedin.com/in") != "linkedin.com" ||
		normalizeDomain("*.com") != "com" {
		t.Fatal("domain helper mismatch")
	}
	if firstDomain([]string{"", "bad/path?query"}) != "" ||
		normalizeDomain("") != "" ||
		normalizeDomain("://bad") != "" ||
		normalizeDomain("https://example.org/path?query") != "" ||
		normalizeDomain("https:///path") != "" ||
		normalizeDomain("example.org?query") != "" {
		t.Fatal("invalid domain helper mismatch")
	}
}

func TestSnippetAndModeHelpers(t *testing.T) {
	long := strings.Repeat("x", snippetRuneCap+10)
	if len([]rune(snippet(long))) != snippetRuneCap {
		t.Fatal("snippet was not bounded")
	}
	var mode inclusionMode
	if err := json.Unmarshal([]byte(`false`), &mode); err != nil || mode != "false" {
		t.Fatalf("boolean mode = %q, %v", mode, err)
	}
	if !inclusionMode("advanced").Enabled() || inclusionMode("false").Enabled() {
		t.Fatal("answer mode enabled mismatch")
	}
	var raw rawContentMode
	if err := json.Unmarshal([]byte(`"markdown"`), &raw); err != nil || !raw.Enabled() {
		t.Fatalf("raw mode = %q, %v", raw, err)
	}
	if err := json.Unmarshal([]byte(`true`), &raw); err != nil || !raw.Enabled() {
		t.Fatalf("raw true mode = %q, %v", raw, err)
	}
	if err := json.Unmarshal([]byte(`false`), &raw); err != nil || raw.Enabled() {
		t.Fatalf("raw false mode = %q, %v", raw, err)
	}
}

func TestResponseOptionHelpers(t *testing.T) {
	if responseAnswer(SearchRequest{}, nil) != nil ||
		responseUsage(SearchRequest{}) != nil ||
		responseAutoParameters(SearchRequest{}, searchcore.Request{}) != nil {
		t.Fatal("disabled response option mismatch")
	}
	if off, ok := responseImages(SearchRequest{}, nil).([]string); !ok || len(off) != 0 {
		t.Fatalf("images without include_images = %#v, want empty string array", off)
	}
	sample := []SearchImage{{URL: "https://a.example/i.png", Description: "alt"}}
	urls, ok := responseImages(SearchRequest{IncludeImages: true}, sample).([]string)
	if !ok || len(urls) != 1 || urls[0] != "https://a.example/i.png" {
		t.Fatalf("images without descriptions = %#v, want URL strings", urls)
	}
	described, ok := responseImages(SearchRequest{
		IncludeImages:            true,
		IncludeImageDescriptions: true,
	}, nil).([]SearchImage)
	if !ok || len(described) != 0 {
		t.Fatalf("described images = %#v, want empty object array", described)
	}
	defaultAuto := responseAutoParameters(
		SearchRequest{AutoParameters: true},
		searchcore.Request{Source: searchcore.SourceLocal},
	)
	if defaultAuto["topic"] != "general" ||
		defaultAuto["search_depth"] != "basic" ||
		defaultAuto["source"] != "local" {
		t.Fatalf("default auto parameters = %#v", defaultAuto)
	}
	if responseFavicon(SearchRequest{IncludeFavicon: true}, "ftp://example.org") != "" {
		t.Fatal("unexpected favicon for unsupported scheme")
	}
	if responseFavicon(SearchRequest{}, "https://example.org/doc") != "" ||
		responseFavicon(SearchRequest{IncludeFavicon: true}, ":bad") != "" ||
		responseFavicon(SearchRequest{IncludeFavicon: true}, "/relative") != "" {
		t.Fatal("favicon helper mismatch")
	}
}

func TestImageResponseHelpers(t *testing.T) {
	doc := documentstore.Document{
		Images: []documentstore.ImageMetadata{
			{URL: "", AltText: "ignored"},
			{URL: "https://example.org/a.png", AltText: "A"},
			{URL: "https://example.org/b.png", AltText: "B"},
			{URL: "https://example.org/c.png", AltText: "C"},
			{URL: "https://example.org/d.png", AltText: "D"},
			{URL: "https://example.org/e.png", AltText: "E"},
			{URL: "https://example.org/f.png", AltText: "F"},
		},
	}
	urls, images := resultImagesFromDocument(
		SearchRequest{IncludeImages: true, IncludeImageDescriptions: true},
		doc,
	)
	if len(urls) != maxResultImages ||
		len(images) != maxResultImages ||
		images[0].Description != "A" {
		t.Fatalf("images urls=%#v details=%#v", urls, images)
	}
	_, withoutDescriptions := resultImagesFromDocument(SearchRequest{IncludeImages: true}, doc)
	if withoutDescriptions[0].Description != "" {
		t.Fatalf("unexpected description = %#v", withoutDescriptions[0])
	}
	if urls, images := resultImagesFromDocument(
		SearchRequest{},
		doc,
	); urls != nil ||
		images != nil {
		t.Fatalf("disabled images urls=%#v details=%#v", urls, images)
	}
	out := make([]SearchImage, maxResponseImages-1)
	got := appendResponseImages(out, []SearchImage{
		{URL: "https://example.org/one.png"},
		{URL: "https://example.org/two.png"},
	})
	if len(got) != maxResponseImages {
		t.Fatalf("response image cap len=%d", len(got))
	}
}

func TestExactAndRawHelpers(t *testing.T) {
	rawValue := "raw"
	if len(exactNeedles(`alpha "beta gamma"`)) != 1 ||
		len(exactNeedles("alpha beta")) != 1 ||
		exactNeedles("  ") != nil ||
		rawContent(nil) != "" ||
		rawContent(&rawValue) != "raw" {
		t.Fatal("exact helper mismatch")
	}
}

func TestGeneratedRequestIDFallback(t *testing.T) {
	previous := randomRead
	randomRead = func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
	t.Cleanup(func() { randomRead = previous })

	if !strings.HasPrefix(generatedRequestID(), "local-") {
		t.Fatal("fallback request id missing local prefix")
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
