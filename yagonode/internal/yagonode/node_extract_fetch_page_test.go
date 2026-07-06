package yagonode

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type pageRoundTrip struct{ body string }

func (rt pageRoundTrip) RoundTrip(*http.Request) (*http.Response, error) {
	header := make(http.Header)
	header.Set("Content-Type", "text/html")

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Header:     header,
	}, nil
}

func TestCrawlPageFetcherAdapter(t *testing.T) {
	config := nodeConfig{}
	config.ExtractFetch.Enabled = true
	fetcher := buildExtractFetcher(config, &http.Client{Transport: pageRoundTrip{
		body: `<html><head><title>T</title></head><body><p>Body text.</p>` +
			`<a href="/next">n</a></body></html>`,
	}})
	pages := crawlPageFetcher(fetcher)
	if pages == nil {
		t.Fatal("page fetcher must adapt")
	}
	page, err := pages.FetchPage(t.Context(), "https://site.example/")
	if err != nil || page.Title != "T" || len(page.Links) != 1 {
		t.Fatalf("page = %+v %v", page, err)
	}

	if crawlPageFetcher(nil) != nil {
		t.Fatal("nil fetcher must stay nil")
	}
}

type failRoundTrip struct{}

func (failRoundTrip) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrHandlerTimeout
}

func TestCrawlPageFetcherAdapterError(t *testing.T) {
	config := nodeConfig{}
	config.ExtractFetch.Enabled = true
	fetcher := buildExtractFetcher(config, &http.Client{Transport: failRoundTrip{}})
	pages := crawlPageFetcher(fetcher)
	if _, err := pages.FetchPage(t.Context(), "https://site.example/"); err == nil {
		t.Fatal("fetch failure must surface")
	}
}
