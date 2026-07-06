package websearch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func htmlResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func fixedClock() func() time.Time {
	clock := time.Unix(1_700_000_000, 0)

	return func() time.Time { return clock }
}

func TestDDGSProviderReturnsResults(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.mojeek.com" {
			t.Errorf("auto should hit Mojeek first, got host %s", r.URL.Host)
		}
		if r.URL.Query().Get("q") != "example" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		return htmlResponse(http.StatusOK, listFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendAuto, CacheTTL: time.Minute, Now: fixedClock(),
	})

	results, err := provider.Search(context.Background(), "example", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 || results[0].URL != "https://example.com/page" {
		t.Fatalf("results = %#v", results)
	}
}

func TestDDGSProviderAutoNeverQueriesDuckDuckGo(t *testing.T) {
	var hosts []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Host)

		return htmlResponse(http.StatusOK, "<html><body>no results</body></html>"), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Backend: backendAuto, Now: fixedClock()})

	if _, err := provider.Search(context.Background(), "example", 10); err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, host := range hosts {
		if strings.Contains(host, "duckduckgo.com") {
			t.Fatalf("auto backend must never query DuckDuckGo, hit %s", host)
		}
	}
	if len(hosts) == 0 {
		t.Fatal("expected auto backend to query at least one engine")
	}
}

func TestDDGSProviderAutoFallsBackToBing(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "www.mojeek.com" {
			return htmlResponse(http.StatusOK, "<html><body>no results</body></html>"), nil
		}
		if r.URL.Host != "www.bing.com" {
			t.Errorf("unexpected host %s", r.URL.Host)
		}
		return htmlResponse(http.StatusOK, listFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Backend: backendAuto, Now: fixedClock()})

	results, err := provider.Search(context.Background(), "example", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
}

func TestDDGSProviderUsesDuckDuckGoWhenSelected(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "html.duckduckgo.com" {
			t.Errorf("host = %s, want html.duckduckgo.com", r.URL.Host)
		}
		return htmlResponse(http.StatusOK, ddgFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendDuckDuckGo, Now: fixedClock(),
	})

	results, err := provider.Search(context.Background(), "example", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/page" {
		t.Fatalf("results = %#v", results)
	}
}

func TestDDGSProviderCachesResponses(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		return htmlResponse(http.StatusOK, listFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendMojeek, CacheTTL: time.Minute, Now: fixedClock(),
	})

	for range 3 {
		if _, err := provider.Search(context.Background(), "example", 10); err != nil {
			t.Fatalf("search: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("transport calls = %d, want 1 (cached)", calls)
	}
}

func TestDDGSProviderBacksOffOnRateLimit(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++

		return htmlResponse(http.StatusAccepted, ""), nil
	})}
	provider := NewDDGSProvider(
		DDGSConfig{Client: client, Backend: backendMojeek, Now: fixedClock()},
	)

	for range 3 {
		results, err := provider.Search(context.Background(), "example", 10)
		if err != nil {
			t.Fatalf("rate limit must degrade, got %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("results = %#v, want empty", results)
		}
	}
	if calls != 1 {
		t.Fatalf("transport calls = %d, want 1 (backed off)", calls)
	}
}

func TestDDGSProviderErrorsOnBadStatus(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponse(http.StatusInternalServerError, ""), nil
	})}
	provider := NewDDGSProvider(
		DDGSConfig{Client: client, Backend: backendMojeek, Now: fixedClock()},
	)

	if _, err := provider.Search(context.Background(), "example", 10); err == nil {
		t.Fatal("expected error on 500 status")
	}
}

func TestDDGSProviderCapsToMaxResults(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponse(http.StatusOK, listFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendMojeek, MaxResults: 1, Now: fixedClock(),
	})

	results, err := provider.Search(context.Background(), "example", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (capped by MaxResults)", len(results))
	}
}

func TestDDGSProviderIgnoresBlankQuery(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run for a blank query")

		return nil, nil
	})}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Now: fixedClock()})

	results, err := provider.Search(context.Background(), "   ", 10)
	if err != nil || results != nil {
		t.Fatalf("results = %#v, err = %v", results, err)
	}
}
