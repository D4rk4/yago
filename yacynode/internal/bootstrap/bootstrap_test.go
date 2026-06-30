package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSeedlistSourceFetchesAllURLsAndSkipsFailures(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1")))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer bad.Close()

	source := New(good.Client(), []string{good.URL, bad.URL})
	seeds := source.Fetch(context.Background())

	if len(seeds) != 1 {
		t.Fatalf("got %d seeds, want 1 (failed source skipped)", len(seeds))
	}
}
