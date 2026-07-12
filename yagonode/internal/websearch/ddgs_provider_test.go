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
		if r.URL.Host != "html.duckduckgo.com" {
			t.Errorf("auto should hit DuckDuckGo first, got host %s", r.URL.Host)
		}
		if r.URL.Query().Get("q") != "example" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		return htmlResponse(http.StatusOK, ddgFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendAuto, CacheTTL: time.Minute, Now: fixedClock(),
	})

	results, err := provider.Search(context.Background(), "example", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/page" {
		t.Fatalf("results = %#v", results)
	}
}

func TestDDGSProviderAutoAsksDuckDuckGoFirst(t *testing.T) {
	var hosts []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Host)
		if r.URL.Host == "html.duckduckgo.com" {
			return htmlResponse(http.StatusOK, ddgFixture), nil
		}

		return htmlResponse(http.StatusOK, "<html><body>no results</body></html>"), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Backend: backendAuto, Now: fixedClock()})

	results, err := provider.Search(context.Background(), "example", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("DuckDuckGo answer discarded")
	}
	if len(hosts) != 1 || hosts[0] != "html.duckduckgo.com" {
		t.Fatalf("an answered query must stop at DuckDuckGo, asked %v", hosts)
	}
}

// TestDDGSProviderSkipsRateLimitedEngineAndWalksOn: a rate-limited DuckDuckGo
// pauses only itself — the same query is answered by the next engine, and the
// next query skips DuckDuckGo without asking it again inside its window.
func TestDDGSProviderSkipsRateLimitedEngineAndWalksOn(t *testing.T) {
	var duckAsks int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Host, "duckduckgo.com") {
			duckAsks++

			return htmlResponse(http.StatusTooManyRequests, ""), nil
		}
		if r.URL.Host == "www.mojeek.com" {
			return htmlResponse(http.StatusOK, listFixture), nil
		}

		return htmlResponse(http.StatusOK, "<html><body>no results</body></html>"), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Backend: backendAuto, Now: fixedClock()})

	for range 2 {
		results, err := provider.Search(context.Background(), "example", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("rate-limited DuckDuckGo must not block the chain")
		}
	}
	if duckAsks != 2 {
		t.Fatalf(
			"DuckDuckGo asked %d times, want 2 (html+lite once, then skipped in backoff)",
			duckAsks,
		)
	}
}

func TestDDGSProviderDoesNotCacheEmptyAnswers(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++

		return htmlResponse(http.StatusOK, "<html><body>no results</body></html>"), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client:   client,
		Backend:  backendMojeek,
		CacheTTL: time.Hour,
		Now:      fixedClock(),
	})

	for range 2 {
		if _, err := provider.Search(context.Background(), "пустой ответ", 10); err != nil {
			t.Fatalf("search: %v", err)
		}
	}
	if attempts != 2 {
		t.Fatalf("engine attempts = %d, want 2 (an empty answer must not be cached)", attempts)
	}
}

func TestDDGSProviderAutoFallsBackToBing(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "www.bing.com" {
			return htmlResponse(http.StatusOK, listFixture), nil
		}

		return htmlResponse(http.StatusOK, "<html><body>no results</body></html>"), nil
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

func TestDDGSProviderCachesConfiguredBoundBeforeCallerCap(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponse(http.StatusOK, listFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendMojeek, MaxResults: 2,
		CacheTTL: time.Minute, Now: fixedClock(),
	})

	results, err := provider.Search(context.Background(), "example", 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (capped by caller)", len(results))
	}
	cached, ok := provider.cache.get("example")
	if !ok || len(cached) != 2 {
		t.Fatalf("cached results = %d, %v, want 2, true", len(cached), ok)
	}
	results, err = provider.Search(context.Background(), "example", 10)
	if err != nil || len(results) != 2 {
		t.Fatalf("configured-cap results = %d, err = %v, want 2, nil", len(results), err)
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

func TestDDGSProviderWalksPastOffTopicEngineAnswers(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "html.duckduckgo.com" {
			return htmlResponse(http.StatusOK, `<!doctype html><html><body>
<div class="result results_links web-result"><div class="links_main">
<a class="result__a" href="https://ru.wiktionary.org/wiki/that">что — Викисловарь</a>
<a class="result__snippet">значение слова</a>
</div></div>
</body></html>`), nil
		}
		if strings.Contains(r.URL.Host, "duckduckgo.com") {
			return htmlResponse(http.StatusOK, `<!doctype html><html><body><table>
<tr><td><a class="result-link" href="https://video.example/ddt">ДДТ - Что такое осень (Official video)</a></td></tr>
<tr><td class="result-snippet">Клип группы ДДТ на песню Что такое осень.</td></tr>
</table></body></html>`), nil
		}

		return htmlResponse(http.StatusOK, "<html><body>no results</body></html>"), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client:  client,
		Backend: backendAuto,
		Now:     fixedClock(),
		Accept:  VerifiedForQuery,
	})

	results, err := provider.Search(context.Background(), "что такое осень ддт", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("off-topic engine answer stopped the walk to DuckDuckGo")
	}
	for _, result := range results {
		if strings.Contains(result.URL, "wiktionary") {
			t.Fatalf("off-topic dictionary row leaked through: %s", result.URL)
		}
	}
}
