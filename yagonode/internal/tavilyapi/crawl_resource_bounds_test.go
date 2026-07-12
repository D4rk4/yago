package tavilyapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

type wideMapFetcher struct {
	calls   int
	payload string
}

func (f *wideMapFetcher) FetchPage(_ context.Context, rawURL string) (CrawledPage, error) {
	f.calls++
	page := CrawledPage{Title: f.payload, Text: f.payload}
	switch {
	case rawURL == "https://map.example/":
		page.Links = make([]string, 100)
		for index := range page.Links {
			page.Links[index] = fmt.Sprintf("https://map.example/p/%d", index)
		}
	case strings.HasPrefix(rawURL, "https://map.example/p/"):
		index, err := strconv.Atoi(strings.TrimPrefix(rawURL, "https://map.example/p/"))
		if err != nil {
			return CrawledPage{}, fmt.Errorf("parse page index: %w", err)
		}
		if index < 99 {
			page.Links = []string{fmt.Sprintf("https://map.example/c/%d", index)}
		}
	}

	return page, nil
}

func TestMapRetainsOnlyTwoHundredBoundedURLs(t *testing.T) {
	backing := strings.Repeat("payload-not-retained", 1<<16)
	fetcher := &wideMapFetcher{payload: backing[10 : 1<<20]}
	limit := crawlMaxLimit
	depth := 2
	breadth := crawlMaxBreadth
	endpoint := crawlEndpoint{fetcher: fetcher, mapOnly: true}
	entries, _, err := endpoint.walk(t.Context(), CrawlRequest{
		URL:        "https://map.example/",
		Limit:      &limit,
		MaxDepth:   &depth,
		MaxBreadth: &breadth,
	})
	if err != nil || len(entries) != crawlMaxLimit || fetcher.calls != crawlMaxLimit {
		t.Fatalf("entries=%d fetches=%d error=%v", len(entries), fetcher.calls, err)
	}
	for _, entry := range entries {
		if entry.RawContent != "" || entry.Favicon != "" ||
			len(entry.URL) > maximumRawContentURLBytes {
			t.Fatalf("retained map entry = %+v", entry)
		}
	}
}

type failureHeavyPageFetcher struct {
	calls int
}

func (f *failureHeavyPageFetcher) FetchPage(
	_ context.Context,
	rawURL string,
) (CrawledPage, error) {
	f.calls++
	if rawURL != "https://failures.example/" {
		return CrawledPage{}, errors.New("fetch failed")
	}
	links := make([]string, 20)
	for index := range links {
		links[index] = fmt.Sprintf("https://failures.example/%d", index)
	}

	return CrawledPage{Text: "root", Links: links}, nil
}

func TestCrawlFailuresConsumeAttemptBudget(t *testing.T) {
	fetcher := &failureHeavyPageFetcher{}
	limit := 10
	breadth := crawlMaxBreadth
	entries, _, err := (crawlEndpoint{fetcher: fetcher}).walk(
		t.Context(),
		CrawlRequest{
			URL: "https://failures.example/", Limit: &limit, MaxBreadth: &breadth,
		},
	)
	if err != nil || len(entries) != 1 || fetcher.calls != limit {
		t.Fatalf("entries=%d fetches=%d error=%v", len(entries), fetcher.calls, err)
	}
}

func TestCrawlURLLengthBoundary(t *testing.T) {
	prefix := "https://length.example/"
	exact := prefix + strings.Repeat("x", maximumRawContentURLBytes-len(prefix))
	limit := 1
	entries, _, err := (crawlEndpoint{fetcher: singlePageFetcher{}}).walk(
		t.Context(), CrawlRequest{URL: exact, Limit: &limit},
	)
	if err != nil || len(entries) != 1 || entries[0].URL != exact {
		t.Fatalf("exact entries=%d error=%v", len(entries), err)
	}
	if _, _, err := (crawlEndpoint{fetcher: singlePageFetcher{}}).walk(
		t.Context(), CrawlRequest{URL: exact + "x", Limit: &limit},
	); !isBadRequest(err) {
		t.Fatalf("plus-one error = %v", err)
	}
	page := CrawledPage{Links: []string{exact, exact + "x"}}
	depth := 1
	discoveredLimit := 2
	entries, _, err = (crawlEndpoint{
		fetcher: sitePages{
			"https://length.example/": page,
			exact:                     {},
		},
	}).walk(t.Context(), CrawlRequest{
		URL: "https://length.example/", Limit: &discoveredLimit, MaxDepth: &depth,
	})
	if err != nil || len(entries) != discoveredLimit || entries[1].URL != exact {
		t.Fatalf("discovered boundary entries=%d error=%v", len(entries), err)
	}
}
