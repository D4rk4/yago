package tavilyapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	for _, want := range []string{`"answer":null`, `"images":[]`, `"follow_up_questions":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %s: %s", want, body)
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"golang","include_images":true}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	endpoint.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"images":["https://example.org/image.png"]`) {
		t.Fatalf("images without descriptions must be URL strings: %s", rec.Body.String())
	}
}

// TestSearchErrorCarriesTavilyDetailEnvelope pins the documented Tavily error
// envelope {"detail":{"error":...}} alongside our structured error object.
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
}
