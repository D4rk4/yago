package yacycrawler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
)

func TestPageFetcherRejectsNonHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		if _, err := w.Write([]byte("%PDF-1.4")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	fetcher := yacycrawler.NewPageFetcher(
		server.Client(),
		yacycrawler.DefaultMaxBodyBytes,
		yacycrawler.DefaultUserAgent,
	)
	if _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
		t.Error("expected non-HTML content type to be rejected")
	}
}

func TestPageFetcherRejectsErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := w.Write([]byte("<html>Just a moment...</html>")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	fetcher := yacycrawler.NewPageFetcher(
		server.Client(),
		yacycrawler.DefaultMaxBodyBytes,
		yacycrawler.DefaultUserAgent,
	)
	if _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
		t.Error("expected error status to be rejected")
	}
}

func TestPageFetcherSendsUserAgent(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte("<html></html>")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	fetcher := yacycrawler.NewPageFetcher(
		server.Client(),
		yacycrawler.DefaultMaxBodyBytes,
		yacycrawler.DefaultUserAgent,
	)
	if _, err := fetcher.Fetch(context.Background(), server.URL); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != yacycrawler.DefaultUserAgent {
		t.Errorf("user agent = %q", got)
	}
}
