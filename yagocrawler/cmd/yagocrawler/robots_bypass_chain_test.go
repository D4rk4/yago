package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/publicweb"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
	"github.com/D4rk4/yago/yagoegress"
)

// TestFetchChainsRobotsDirectVariantsSkipRobots is the CRAWL-04 leftover
// acceptance: the default chain refuses a robots-disallowed path while the
// IgnoreRobots variant of the same chain fetches it.
func TestFetchChainsRobotsDirectVariantsSkipRobots(t *testing.T) {
	restoreAssemblySeams(t)
	newCrawlerPublicWebAdmissionFetcher = func(
		inner pagefetch.PageSource,
		_ publicweb.Resolver,
		_ yagoegress.Guard,
	) pagefetch.PageSource {
		return inner
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>private page words</body></html>"))
	}))
	defer origin.Close()

	crawl := serviceConfig().Crawl
	chains, err := buildFetchChains(
		yagoegress.NewGuard(true),
		origin.Client(),
		crawl,
		htmlPageSource(map[string]string{}),
		crawlermetrics.New(),
	)
	if err != nil {
		t.Fatalf("buildFetchChains: %v", err)
	}

	target, err := url.Parse(origin.URL + "/private")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if _, err := chains.verifying.Fetch(
		context.Background(),
		target,
	); !errors.Is(err, robots.ErrDisallowed) {
		t.Fatalf("verifying chain error = %v, want robots denial", err)
	}
	page, err := chains.verifyingDirect.Fetch(context.Background(), target)
	if err != nil {
		t.Fatalf("direct chain fetch: %v", err)
	}
	if !strings.Contains(string(page.Body), "private page words") {
		t.Fatalf("direct chain body = %q", page.Body)
	}
}
