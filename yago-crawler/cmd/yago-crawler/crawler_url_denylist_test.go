package main

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlURLDenylistWrapsEveryFetchChain(t *testing.T) {
	denylist := crawldenylist.New()
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(
		[]string{"https://blocked.example/exact"},
		[]string{"denied.example"},
	)
	if err != nil {
		t.Fatalf("NewCrawlURLDenylist: %v", err)
	}
	if err := denylist.Apply(policy); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	calls := 0
	inner := pageSourceFunc(func(
		_ context.Context,
		target *url.URL,
	) (pagefetch.FetchedPage, error) {
		calls++

		return pagefetch.FetchedPage{URL: target}, nil
	})
	chains := applyCrawlURLDenylist(fetchChains{
		verifying:       inner,
		insecure:        inner,
		verifyingDirect: inner,
		insecureDirect:  inner,
	}, denylist)
	targets := []*url.URL{
		mustParseURL(t, "https://blocked.example/exact"),
		mustParseURL(t, "https://sub.denied.example/page"),
	}
	sources := []pagefetch.PageSource{
		chains.verifying,
		chains.insecure,
		chains.verifyingDirect,
		chains.insecureDirect,
	}
	for index, source := range sources {
		_, err := source.Fetch(t.Context(), targets[index%len(targets)])
		if !errors.Is(err, pagefetch.ErrPageRejected) {
			t.Fatalf("source %d error = %v", index, err)
		}
	}
	if calls != 0 {
		t.Fatalf("inner fetch calls = %d, want 0", calls)
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", rawURL, err)
	}

	return parsed
}
