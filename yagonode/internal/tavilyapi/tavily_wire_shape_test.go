package tavilyapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// TestSearchWireShapeMatchesTavilyDefaults pins the raw JSON contract a real
// Tavily client relies on: answer, images, and follow_up_questions are present
// on every response (null / [] when not requested), and without
// include_image_descriptions the images array holds plain URL strings.
func TestSearchWireShapeMatchesTavilyDefaults(t *testing.T) {
	endpoint, _, _ := richSearchEndpoint()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"golang"}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"answer":null`, `"images":[]`, `"follow_up_questions":null`,
		`"raw_content":null`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"source"`) || strings.Contains(body, `"published_date"`) {
		t.Fatalf("default response exposed non-reference result fields: %s", body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(
			`{"query":"golang","include_images":true,"include_image_descriptions":true}`,
		),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(
		rec.Body.String(),
		`"images":[{"url":"https://example.org/image.png","description":"Document image"}]`,
	) {
		t.Fatalf("described images must be objects: %s", rec.Body.String())
	}
}

func TestSearchErrorCarriesTavilyDetailEnvelope(t *testing.T) {
	endpoint, _, _ := richSearchEndpoint()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":""}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"detail":{"error":"query is required"}`) {
		t.Fatalf("missing Tavily detail envelope: %s", rec.Body.String())
	}
	if rec.Body.String() != "{\"detail\":{\"error\":\"query is required\"}}\n" {
		t.Fatalf("unexpected error fields: %s", rec.Body.String())
	}
}

func TestSearchRequestedEmptyImagesArePresent(t *testing.T) {
	search := &fakeSearcher{response: searchcore.Response{Results: []searchcore.Result{{
		Title: "No image", URL: "https://no-image.example/", Snippet: "golang content",
	}}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"golang","include_images":true}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	newTestSearchEndpoint(search, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK ||
		strings.Count(rec.Body.String(), `"images":[]`) != 2 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
