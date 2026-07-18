package httpfetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
)

func TestPageFetcherCapturesXRobotsTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	page, err := httpfetch.NewPageFetcher(server.Client(), "", 0).
		Fetch(context.Background(), mustParse(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.RobotsTag != "noindex, nofollow" {
		t.Fatalf("robots tag = %q", page.RobotsTag)
	}
}

func TestPageFetcherLeavesRobotsTagEmptyWhenAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	page, err := httpfetch.NewPageFetcher(server.Client(), "", 0).
		Fetch(context.Background(), mustParse(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.RobotsTag != "" {
		t.Fatalf("robots tag = %q, want empty", page.RobotsTag)
	}
}
