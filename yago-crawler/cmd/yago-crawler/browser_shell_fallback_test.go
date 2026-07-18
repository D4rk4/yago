package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/publicweb"
	"github.com/D4rk4/yago/yagoegress"
)

func TestFetchChainsRenderOnlySuccessfulHTMLShells(t *testing.T) {
	restoreAssemblySeams(t)
	newCrawlerPublicWebAdmissionFetcher = func(
		inner pagefetch.PageSource,
		_ publicweb.Resolver,
		_ yagoegress.Guard,
	) pagefetch.PageSource {
		return inner
	}
	origin := httptest.NewServer(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.Path == "/shell" {
				_, _ = response.Write(
					[]byte(
						`<html><head><title>Portal</title></head><body><div></div><script src="/app.js"></script></body></html>`,
					),
				)

				return
			}
			_, _ = response.Write(
				[]byte(
					`<html><body><article>Static HTML already contains complete useful searchable page content.</article><script src="/metrics.js"></script></body></html>`,
				),
			)
		}),
	)
	defer origin.Close()

	browserCalls := 0
	browser := pageSourceFunc(func(
		_ context.Context,
		target *url.URL,
	) (pagefetch.FetchedPage, error) {
		browserCalls++

		return pagefetch.FetchedPage{
			URL:         target,
			ContentType: "text/html",
			Body: []byte(
				"<html><body>Rendered application content is now searchable.</body></html>",
			),
		}, nil
	})
	chains, err := buildFetchChains(
		yagoegress.NewGuard(true),
		origin.Client(),
		DefaultCrawlConfig(),
		browser,
		crawlermetrics.New(),
	)
	if err != nil {
		t.Fatalf("buildFetchChains: %v", err)
	}
	shellURL, err := url.Parse(origin.URL + "/shell")
	if err != nil {
		t.Fatalf("parse shell URL: %v", err)
	}
	shell, err := chains.verifyingDirect.Fetch(t.Context(), shellURL)
	if err != nil {
		t.Fatalf("fetch shell: %v", err)
	}
	if browserCalls != 1 || !strings.Contains(string(shell.Body), "Rendered application") {
		t.Fatalf("shell/browser calls = %q/%d", shell.Body, browserCalls)
	}
	staticURL, err := url.Parse(origin.URL + "/static")
	if err != nil {
		t.Fatalf("parse static URL: %v", err)
	}
	static, err := chains.verifyingDirect.Fetch(t.Context(), staticURL)
	if err != nil {
		t.Fatalf("fetch static HTML: %v", err)
	}
	if browserCalls != 1 || !strings.Contains(string(static.Body), "Static HTML") {
		t.Fatalf("static/browser calls = %q/%d", static.Body, browserCalls)
	}
}
