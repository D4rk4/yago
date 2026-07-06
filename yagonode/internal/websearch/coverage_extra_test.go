package websearch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// deepNestedHTML exceeds x/net/html's 512-element open-stack limit, forcing
// html.Parse to return an error rather than a tree.
func deepNestedHTML() string { return strings.Repeat("<div>", 600) }

func TestBackendsForBing(t *testing.T) {
	engines := backendsFor(backendBing)
	if len(engines) != 1 || engines[0].name != engineBing {
		t.Fatalf("backendsFor(bing) = %#v", engines)
	}
}

func TestMojeekSafeParamsOff(t *testing.T) {
	if got := mojeekSafeParams("off").Get("safe"); got != "0" {
		t.Fatalf("safe = %q, want 0", got)
	}
	if got := mojeekSafeParams("on").Get("safe"); got != "1" {
		t.Fatalf("safe = %q, want 1", got)
	}
}

func TestDuckSafeParams(t *testing.T) {
	if got := duckSafeParams("strict").Get("kp"); got != "1" {
		t.Fatalf("strict kp = %q, want 1", got)
	}
	if got := duckSafeParams("off").Get("kp"); got != "-1" {
		t.Fatalf("off kp = %q, want -1", got)
	}
	if got := duckSafeParams("moderate").Get("kp"); got != "" {
		t.Fatalf("moderate kp = %q, want empty", got)
	}
}

func TestParseListResultsHTMLError(t *testing.T) {
	if _, err := parseListResults([]byte(deepNestedHTML())); err == nil {
		t.Fatal("expected parse error on deeply nested HTML")
	}
}

func TestParseListResultsSkipsItemWithoutTitleLink(t *testing.T) {
	const fixture = `<ul><li><div>no title link here</div></li></ul>`
	results, err := parseListResults([]byte(fixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want none", results)
	}
}

func TestParseDuckDuckGoResultsHTMLError(t *testing.T) {
	if _, err := parseDuckDuckGoResults([]byte(deepNestedHTML())); err == nil {
		t.Fatal("expected parse error on deeply nested HTML")
	}
}

func TestParseDuckDuckGoResultsSkipsMalformed(t *testing.T) {
	const fixture = `<!doctype html><html><body>
<div class="result"><p>container without a result link</p></div>
<div class="result"><a class="result__a" href="/relative-only">Relative</a></div>
</body></html>`
	results, err := parseDuckDuckGoResults([]byte(fixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want none (no link and unresolvable href)", results)
	}
}

func TestParseDuckDuckGoLiteResultsHTMLError(t *testing.T) {
	if _, err := parseDuckDuckGoLiteResults([]byte(deepNestedHTML())); err == nil {
		t.Fatal("expected parse error on deeply nested HTML")
	}
}

func TestUnwrapRedirectEmptyAndMalformed(t *testing.T) {
	if got := unwrapRedirect(""); got != "" {
		t.Fatalf("unwrapRedirect(empty) = %q, want empty", got)
	}
	if got := unwrapRedirect("http://\x7f/ctrl"); got != "" {
		t.Fatalf("unwrapRedirect(ctrl) = %q, want empty", got)
	}
}

func TestNewDDGSProviderDefaults(t *testing.T) {
	provider := NewDDGSProvider(DDGSConfig{})
	if provider.now == nil {
		t.Fatal("now defaulted to nil")
	}
	if provider.client != http.DefaultClient {
		t.Fatalf("client = %#v, want http.DefaultClient", provider.client)
	}
}

func TestDDGSProviderFetchAppliesTimeout(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("timeout should set a request deadline")
		}
		return htmlResponse(http.StatusOK, listFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendMojeek, Timeout: time.Minute, Now: fixedClock(),
	})

	results, err := provider.Search(context.Background(), "example", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
}

func TestDDGSProviderFetchRequestBuildError(t *testing.T) {
	provider := NewDDGSProvider(DDGSConfig{Backend: backendMojeek, Now: fixedClock()})
	bad := engine{name: "bad", endpoint: "http://\x7f", queryKey: "q"}

	if _, _, err := provider.fetch(context.Background(), bad, "example"); err == nil {
		t.Fatal("expected request build error for malformed endpoint")
	}
}

func TestDDGSProviderErrorsOnTransportError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial refused")
	})}
	provider := NewDDGSProvider(
		DDGSConfig{Client: client, Backend: backendMojeek, Now: fixedClock()},
	)

	if _, err := provider.Search(context.Background(), "example", 10); err == nil {
		t.Fatal("expected error when transport fails")
	}
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read boom") }
func (errReadCloser) Close() error             { return nil }

func TestDDGSProviderErrorsOnBodyReadError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errReadCloser{},
			Header:     make(http.Header),
		}, nil
	})}
	provider := NewDDGSProvider(
		DDGSConfig{Client: client, Backend: backendMojeek, Now: fixedClock()},
	)

	if _, err := provider.Search(context.Background(), "example", 10); err == nil {
		t.Fatal("expected error when reading the body fails")
	}
}

func TestDDGSProviderErrorsOnParseError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponse(http.StatusOK, deepNestedHTML()), nil
	})}
	provider := NewDDGSProvider(
		DDGSConfig{Client: client, Backend: backendMojeek, Now: fixedClock()},
	)

	if _, err := provider.Search(context.Background(), "example", 10); err == nil {
		t.Fatal("expected error when the parser rejects the body")
	}
}

func TestRecordBackoffDoublesAndCaps(t *testing.T) {
	provider := NewDDGSProvider(DDGSConfig{Backend: backendMojeek, Now: fixedClock()})

	provider.recordBackoff()
	if provider.backoff != minBackoff {
		t.Fatalf("first backoff = %v, want %v", provider.backoff, minBackoff)
	}
	provider.recordBackoff()
	if provider.backoff != 2*minBackoff {
		t.Fatalf("second backoff = %v, want %v", provider.backoff, 2*minBackoff)
	}
	for range 12 {
		provider.recordBackoff()
	}
	if provider.backoff != maxBackoff {
		t.Fatalf("capped backoff = %v, want %v", provider.backoff, maxBackoff)
	}
}

func TestResultHostRejectsMalformedURL(t *testing.T) {
	if host := resultHost("http://\x7f/ctrl"); host != "" {
		t.Fatalf("resultHost(malformed) = %q, want empty", host)
	}
}

func TestQueryCacheEvictsExpiredOnPut(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Minute, 1, func() time.Time { return clock })

	cache.put("a", []Result{{Title: "a"}})
	clock = clock.Add(2 * time.Minute)
	cache.put("b", []Result{{Title: "b"}})

	if _, ok := cache.get("a"); ok {
		t.Fatal("expired entry should have been evicted")
	}
	if got, ok := cache.get("b"); !ok || len(got) != 1 || got[0].Title != "b" {
		t.Fatalf("get(b) = %#v, %v", got, ok)
	}
}
