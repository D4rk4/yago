package yacycrawler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func TestPolitenessGateBlocksDisallowedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			if _, err := w.Write([]byte("User-agent: *\nDisallow: /private\n")); err != nil {
				t.Errorf("write robots: %v", err)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
	}))
	defer server.Close()

	gate := yacycrawler.NewPolitenessGate(
		server.Client(),
		yacycrawler.DefaultUserAgent,
		time.Millisecond,
	)
	ctx := context.Background()

	allowed, err := gate.Allow(ctx, server.URL+"/private/secret")
	if err != nil {
		t.Fatalf("allow disallowed: %v", err)
	}
	if allowed {
		t.Error("expected /private to be disallowed")
	}

	allowed, err = gate.Allow(ctx, server.URL+"/public")
	if err != nil {
		t.Fatalf("allow public: %v", err)
	}
	if !allowed {
		t.Error("expected /public to be allowed")
	}
}

func TestPolitePageFetcherRejectsDisallowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			if _, err := w.Write([]byte("User-agent: *\nDisallow: /\n")); err != nil {
				t.Errorf("write robots: %v", err)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
	}))
	defer server.Close()

	fetcher := yacycrawler.NewPageFetcher(
		server.Client(),
		yacycrawler.DefaultMaxBodyBytes,
		yacycrawler.DefaultUserAgent,
	)
	gate := yacycrawler.NewPolitenessGate(
		server.Client(),
		yacycrawler.DefaultUserAgent,
		time.Millisecond,
	)
	polite := yacycrawler.NewPolitePageFetcher(fetcher, gate)

	if _, err := polite.Fetch(context.Background(), server.URL+"/anything"); err == nil {
		t.Error("expected fetch to be blocked by robots.txt")
	}
}
