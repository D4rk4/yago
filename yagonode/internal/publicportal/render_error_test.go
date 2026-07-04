package publicportal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type failingResponseWriter struct{ header http.Header }

func (f *failingResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}

	return f.header
}

func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *failingResponseWriter) WriteHeader(int) {}

func TestOpenSearchRoutes(t *testing.T) {
	t.Parallel()

	os := NewOpenSearch()
	if os.DescribePath() != osddPath {
		t.Fatalf("DescribePath = %q, want %q", os.DescribePath(), osddPath)
	}
	if os.SuggestPath() != suggestPath {
		t.Fatalf("SuggestPath = %q, want %q", os.SuggestPath(), suggestPath)
	}
}

func TestOpenSearchSuggestSurvivesEncodeFailure(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://node.example/opensearch/suggest?q=cats",
		nil,
	)
	NewOpenSearch().Suggest(&failingResponseWriter{}, req)
}

func TestPortalRenderSurvivesWriteFailure(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	New(fakeSource{}).ServeHTTP(&failingResponseWriter{}, req)
}
