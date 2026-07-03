package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
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

func extractHandler(rows map[string]documentstore.Document) http.Handler {
	return NewExtractEndpointWithAccess(&fakeDocuments{rows: rows}, SearchAccessPolicy{})
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
	rec := postExtract(t, extractHandler(nil), "{", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var envelope ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	if envelope.Error.Code != "invalid_extract_request" {
		t.Fatalf("code = %q", envelope.Error.Code)
	}
}

func TestExtractRejectsNonStringURLs(t *testing.T) {
	rec := postExtract(t, extractHandler(nil), `{"urls":123}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var envelope ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	if !strings.Contains(envelope.Error.Message, "urls must be") {
		t.Fatalf("message = %q", envelope.Error.Message)
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
			rec := postExtract(t, extractHandler(nil), body, "")
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
	rec := postExtract(t, extractHandler(rows), `{"urls":"http://ex.com/a"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeExtract(t, rec)
	if len(resp.Results) != 1 || len(resp.FailedResults) != 0 {
		t.Fatalf("results=%d failed=%d", len(resp.Results), len(resp.FailedResults))
	}
	if resp.Results[0].URL != "http://ex.com/a" ||
		resp.Results[0].RawContent != "the quick brown fox" {
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
		postExtract(t, handler, `{"urls":"http://ex.com/full","format":"markdown"}`, ""),
	)
	if full.Results[0].RawContent != "# Title\n\nbody text" {
		t.Fatalf("full markdown = %q", full.Results[0].RawContent)
	}
	titleOnly := decodeExtract(
		t,
		postExtract(t, handler, `{"urls":"http://ex.com/title","format":"MarkDown"}`, ""),
	)
	if titleOnly.Results[0].RawContent != "# Only Title" {
		t.Fatalf("title-only markdown = %q", titleOnly.Results[0].RawContent)
	}
	textOnly := decodeExtract(
		t,
		postExtract(t, handler, `{"urls":"http://ex.com/text","format":"markdown"}`, ""),
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
		"",
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
	resp := decodeExtract(t, postExtract(t, extractHandler(rows), body, ""))
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
	resp := decodeExtract(t, postExtract(t, extractHandler(rows), body, ""))
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
			"",
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
			"",
		),
	)
	if resp.Results[0].Images != nil {
		t.Fatalf("images = %v, want nil", resp.Results[0].Images)
	}
}

func TestExtractReturnsStoreError(t *testing.T) {
	handler := NewExtractEndpointWithAccess(
		&fakeDocuments{err: errors.New("store unavailable")},
		SearchAccessPolicy{},
	)
	rec := postExtract(t, handler, `{"urls":"http://ex.com/a"}`, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestExtractNilDirectoryMissesEveryURL(t *testing.T) {
	handler := NewExtractEndpoint(nil)
	resp := decodeExtract(t, postExtract(t, handler, `{"urls":"http://ex.com/a"}`, ""))
	if len(resp.Results) != 0 || len(resp.FailedResults) != 1 {
		t.Fatalf("results=%d failed=%d", len(resp.Results), len(resp.FailedResults))
	}
}

func TestMountExtractRegistersRoute(t *testing.T) {
	mux := http.NewServeMux()
	rows := map[string]documentstore.Document{"http://ex.com/a": {ExtractedText: "hi"}}
	MountExtract(mux, &fakeDocuments{rows: rows}, SearchAccessPolicy{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		PathExtract,
		strings.NewReader(`{"urls":"http://ex.com/a"}`),
	)
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
