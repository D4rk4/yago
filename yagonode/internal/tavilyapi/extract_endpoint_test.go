package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func postExtract(
	t *testing.T,
	handler http.Handler,
	body, bearer string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		PathExtract,
		strings.NewReader(body),
	)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func decodeExtract(t *testing.T, rec *httptest.ResponseRecorder) ExtractResponse {
	t.Helper()
	var resp ExtractResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode extract response: %v; body=%s", err, rec.Body.String())
	}

	return resp
}

const extractTestKey = "extract-test-key"

func extractHandler(rows map[string]documentstore.Document) http.Handler {
	return NewExtractEndpointWithAccess(
		&fakeDocuments{rows: rows},
		SearchAccessPolicy{BearerToken: extractTestKey},
	)
}

func TestExtractRejectsNonPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, PathExtract, nil)
	extractHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", rec.Header().Get("Allow"))
	}
}

func TestExtractRequiresBearerWhenConfigured(t *testing.T) {
	handler := NewExtractEndpointWithAccess(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: "secret"},
	)
	rec := postExtract(t, handler, `{"urls":"http://ex.com/a"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
}

func TestExtractAllowsWhenAuthorized(t *testing.T) {
	rows := map[string]documentstore.Document{"http://ex.com/a": {ExtractedText: "hello"}}
	handler := NewExtractEndpointWithAccess(
		&fakeDocuments{rows: rows},
		SearchAccessPolicy{BearerToken: "secret"},
	)
	rec := postExtract(t, handler, `{"urls":"http://ex.com/a"}`, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestExtractRejectsInvalidJSON(t *testing.T) {
	rec := postExtract(t, extractHandler(nil), "{", extractTestKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var envelope ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	if envelope.Detail.Error != "invalid extract request" {
		t.Fatalf("detail = %q", envelope.Detail.Error)
	}
}

func TestExtractRejectsNonStringURLs(t *testing.T) {
	rec := postExtract(t, extractHandler(nil), `{"urls":123}`, extractTestKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var envelope ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	if !strings.Contains(envelope.Detail.Error, "urls must be") {
		t.Fatalf("message = %q", envelope.Detail.Error)
	}
}

func TestExtractValidationErrors(t *testing.T) {
	tooMany := make([]string, maxExtractURLs+1)
	for i := range tooMany {
		tooMany[i] = `"http://ex.com/a"`
	}
	cases := map[string]string{
		"missing urls":       `{}`,
		"empty urls":         `{"urls":[]}`,
		"too many urls":      `{"urls":[` + strings.Join(tooMany, ",") + `]}`,
		"unsupported depth":  `{"urls":"http://ex.com/a","extract_depth":"deep"}`,
		"unsupported format": `{"urls":"http://ex.com/a","format":"html"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postExtract(t, extractHandler(nil), body, extractTestKey)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestExtractReturnsCachedContent(t *testing.T) {
	rows := map[string]documentstore.Document{
		"http://ex.com/a": {ExtractedText: "the quick brown fox", Title: "Fox"},
	}
	rec := postExtract(t, extractHandler(rows), `{"urls":"http://ex.com/a"}`, extractTestKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeExtract(t, rec)
	if len(resp.Results) != 1 || len(resp.FailedResults) != 0 {
		t.Fatalf("results=%d failed=%d", len(resp.Results), len(resp.FailedResults))
	}
	if resp.Results[0].URL != "http://ex.com/a" ||
		resp.Results[0].RawContent != "# Fox\n\nthe quick brown fox" {
		t.Fatalf("unexpected result: %+v", resp.Results[0])
	}
	if resp.RequestID == "" {
		t.Fatal("request_id must be present")
	}
}

func TestExtractMarkdownFormat(t *testing.T) {
	rows := map[string]documentstore.Document{
		"http://ex.com/full":  {Title: "Title", ExtractedText: "body text"},
		"http://ex.com/title": {Title: "Only Title"},
		"http://ex.com/text":  {ExtractedText: "only text"},
	}
	handler := extractHandler(rows)

	full := decodeExtract(
		t,
		postExtract(
			t,
			handler,
			`{"urls":"http://ex.com/full","format":"markdown"}`,
			extractTestKey,
		),
	)
	if full.Results[0].RawContent != "# Title\n\nbody text" {
		t.Fatalf("full markdown = %q", full.Results[0].RawContent)
	}
	titleOnly := decodeExtract(
		t,
		postExtract(
			t,
			handler,
			`{"urls":"http://ex.com/title","format":"MarkDown"}`,
			extractTestKey,
		),
	)
	if titleOnly.Results[0].RawContent != "# Only Title" {
		t.Fatalf("title-only markdown = %q", titleOnly.Results[0].RawContent)
	}
	textOnly := decodeExtract(
		t,
		postExtract(
			t,
			handler,
			`{"urls":"http://ex.com/text","format":"markdown"}`,
			extractTestKey,
		),
	)
	if textOnly.Results[0].RawContent != "only text" {
		t.Fatalf("text-only markdown = %q", textOnly.Results[0].RawContent)
	}
}

func TestExtractStripsFragmentAndAcceptsTextFormat(t *testing.T) {
	rows := map[string]documentstore.Document{"http://ex.com/a": {ExtractedText: "content"}}
	rec := postExtract(
		t,
		extractHandler(rows),
		`{"urls":"http://ex.com/a#section","format":"text"}`,
		extractTestKey,
	)
	resp := decodeExtract(t, rec)
	if len(resp.Results) != 1 || resp.Results[0].RawContent != "content" {
		t.Fatalf("fragment lookup failed: %+v", resp)
	}
	if resp.Results[0].URL != "http://ex.com/a#section" {
		t.Fatalf("result url should echo the request: %q", resp.Results[0].URL)
	}
}

func TestExtractSeparatesFoundInvalidAndMissing(t *testing.T) {
	rows := map[string]documentstore.Document{"http://ex.com/a": {ExtractedText: "hit"}}
	body := `{"urls":["http://ex.com/a","http://ex.com/missing","ftp://ex.com/x","http://[::1","http:///nohost","notaurl"],"extract_depth":"advanced"}`
	resp := decodeExtract(t, postExtract(t, extractHandler(rows), body, extractTestKey))
	if len(resp.Results) != 1 || resp.Results[0].URL != "http://ex.com/a" {
		t.Fatalf("results = %+v", resp.Results)
	}
	if len(resp.FailedResults) != 5 {
		t.Fatalf("failed_results = %d, want 5: %+v", len(resp.FailedResults), resp.FailedResults)
	}
}

func TestExtractIncludesImagesAndFavicon(t *testing.T) {
	rows := map[string]documentstore.Document{
		"http://ex.com/a": {
			ExtractedText: "content",
			Images: []documentstore.ImageMetadata{
				{URL: "http://ex.com/1.png", AltText: "one"},
				{URL: "http://ex.com/2.png"},
			},
		},
	}
	body := `{"urls":"http://ex.com/a","include_images":true,"include_favicon":true}`
	resp := decodeExtract(t, postExtract(t, extractHandler(rows), body, extractTestKey))
	got := resp.Results[0]
	if len(got.Images) != 2 || got.Images[0] != "http://ex.com/1.png" {
		t.Fatalf("images = %v", got.Images)
	}
	if got.Favicon != "http://ex.com/favicon.ico" {
		t.Fatalf("favicon = %q", got.Favicon)
	}
}

func TestExtractCapsAndSkipsImages(t *testing.T) {
	images := make([]documentstore.ImageMetadata, 0, maxResultImages+3)
	images = append(images, documentstore.ImageMetadata{URL: ""})
	for i := 0; i < maxResultImages+2; i++ {
		images = append(images, documentstore.ImageMetadata{URL: "http://ex.com/i.png"})
	}
	rows := map[string]documentstore.Document{
		"http://ex.com/a": {ExtractedText: "c", Images: images},
	}
	resp := decodeExtract(
		t,
		postExtract(
			t,
			extractHandler(rows),
			`{"urls":"http://ex.com/a","include_images":true}`,
			extractTestKey,
		),
	)
	if len(resp.Results[0].Images) != maxResultImages {
		t.Fatalf("images = %d, want %d", len(resp.Results[0].Images), maxResultImages)
	}
}

func TestExtractOmitsImagesWhenDocumentHasNone(t *testing.T) {
	rows := map[string]documentstore.Document{"http://ex.com/a": {ExtractedText: "c"}}
	resp := decodeExtract(
		t,
		postExtract(
			t,
			extractHandler(rows),
			`{"urls":"http://ex.com/a","include_images":true}`,
			extractTestKey,
		),
	)
	if len(resp.Results[0].Images) != 0 ||
		!strings.Contains(
			postExtract(
				t,
				extractHandler(rows),
				`{"urls":"http://ex.com/a","include_images":true}`,
				extractTestKey,
			).Body.String(),
			`"images":[]`,
		) {
		t.Fatalf("images = %v, want an empty wire array", resp.Results[0].Images)
	}
}

func TestExtractReturnsStoreError(t *testing.T) {
	handler := NewExtractEndpointWithAccess(
		&fakeDocuments{err: errors.New("store unavailable")},
		SearchAccessPolicy{BearerToken: extractTestKey},
	)
	rec := postExtract(t, handler, `{"urls":"http://ex.com/a"}`, extractTestKey)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestExtractNilDirectoryMissesEveryURL(t *testing.T) {
	handler := NewExtractEndpointWithAccess(nil, SearchAccessPolicy{BearerToken: extractTestKey})
	resp := decodeExtract(
		t,
		postExtract(t, handler, `{"urls":"http://ex.com/a"}`, extractTestKey),
	)
	if len(resp.Results) != 0 || len(resp.FailedResults) != 1 {
		t.Fatalf("results=%d failed=%d", len(resp.Results), len(resp.FailedResults))
	}
}

// TestExtractDefaultConstructorDeniesAnonymous pins SEC-02: an endpoint built
// without any credential serves nothing rather than everything.
func TestExtractDefaultConstructorDeniesAnonymous(t *testing.T) {
	rec := postExtract(t, NewExtractEndpoint(nil), `{"urls":"http://ex.com/a"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no key, no access)", rec.Code)
	}
}

func TestMountExtractRegistersRoute(t *testing.T) {
	mux := http.NewServeMux()
	rows := map[string]documentstore.Document{"http://ex.com/a": {ExtractedText: "hi"}}
	MountExtract(
		mux,
		&fakeDocuments{rows: rows},
		SearchAccessPolicy{BearerToken: extractTestKey},
		nil,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		PathExtract,
		strings.NewReader(`{"urls":"http://ex.com/a"}`),
	)
	req.Header.Set("Authorization", "Bearer "+extractTestKey)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestFaviconURL(t *testing.T) {
	cases := map[string]string{
		"http://ex.com/a":  "http://ex.com/favicon.ico",
		"https://ex.com/a": "https://ex.com/favicon.ico",
		"ftp://ex.com/a":   "",
		"http://[::1":      "",
		"http:///nohost":   "",
	}
	for in, want := range cases {
		if got := faviconURL(in); got != want {
			t.Fatalf("faviconURL(%q) = %q, want %q", in, got, want)
		}
	}
}
