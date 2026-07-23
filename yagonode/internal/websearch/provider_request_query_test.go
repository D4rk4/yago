package websearch

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestColdScopedProviderQueryFiltersBeforeEngineSelectionAndCachesSeparately(
	t *testing.T,
) {
	var mutex sync.Mutex
	var queries []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (
		*http.Response,
		error,
	) {
		mutex.Lock()
		queries = append(queries, request.URL.Query().Get("q"))
		mutex.Unlock()

		return htmlResponse(http.StatusOK, "answer"), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, CacheTTL: time.Minute, Now: fixedClock(),
		Accept: VerifiedForQuery,
	})
	provider.engines = []engine{
		tracingEngine(
			"first",
			Result{Title: "Unrelated", URL: "https://outside.example/reference"},
		),
		tracingEngine(
			"second",
			Result{Title: "Reference manual", URL: "https://postgresql.org/current/"},
		),
	}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)
	scoped := searchcore.Request{
		Query:          "PostgreSQL official documentation",
		Source:         searchcore.SourceGlobal,
		Limit:          10,
		Verify:         searchcore.VerifyFalse,
		IncludeDomains: []string{"postgresql.org"},
	}

	for range 2 {
		response, err := searcher.Search(t.Context(), scoped)
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Results) != 1 ||
			response.Results[0].URL != "https://postgresql.org/current/" {
			t.Fatalf("scoped results = %#v", response.Results)
		}
	}
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query:  scoped.Query,
		Limit:  scoped.Limit,
		Verify: searchcore.VerifyFalse,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != "https://outside.example/reference" {
		t.Fatalf("unscoped results = %#v", response.Results)
	}

	mutex.Lock()
	defer mutex.Unlock()
	wantQueries := []string{
		"PostgreSQL official documentation site:postgresql.org",
		"PostgreSQL official documentation site:postgresql.org",
		"PostgreSQL official documentation",
	}
	if len(queries) != len(wantQueries) {
		t.Fatalf("provider queries = %q, want %q", queries, wantQueries)
	}
	for index := range wantQueries {
		if queries[index] != wantQueries[index] {
			t.Fatalf("provider queries = %q, want %q", queries, wantQueries)
		}
	}
}

func TestRequestVerifyFalseOverridesConfiguredLexicalAcceptance(t *testing.T) {
	var secondCalls atomic.Int32
	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (
			*http.Response,
			error,
		) {
			if request.URL.Host == "second.example" {
				secondCalls.Add(1)
			}

			return htmlResponse(http.StatusOK, "answer"), nil
		})},
		Accept: VerifiedForQuery,
		Now:    fixedClock(),
	})
	provider.engines = []engine{
		tracingEngine(
			"first",
			Result{Title: "Mature animal mass", URL: "https://reference.example/"},
		),
		tracingEngine(
			"second",
			Result{Title: "Adult giraffe weight", URL: "https://second.example/"},
		),
	}

	response, err := NewFallbackSearcher(&stubSearcher{}, provider, enabled).Search(
		t.Context(),
		searchcore.Request{
			Query: "adult giraffe weight", Source: searchcore.SourceGlobal,
			Limit: 10, Verify: searchcore.VerifyFalse,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != "https://reference.example/" {
		t.Fatalf("results = %#v", response.Results)
	}
	if secondCalls.Load() != 0 {
		t.Fatalf("second engine calls = %d, want zero", secondCalls.Load())
	}
}

func TestRequestVerifyIfExistRemainsAuthoritativeDuringEngineSelection(t *testing.T) {
	var secondCalls atomic.Int32
	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (
			*http.Response,
			error,
		) {
			if request.URL.Host == "second.example" {
				secondCalls.Add(1)
			}

			return htmlResponse(http.StatusOK, "answer"), nil
		})},
		Accept: VerifiedForQuery,
		Now:    fixedClock(),
	})
	provider.engines = []engine{
		tracingEngine(
			"first",
			Result{Title: "alpha", URL: "https://first.example/"},
		),
		tracingEngine(
			"second",
			Result{Title: "alpha beta", URL: "https://second.example/"},
		),
	}

	response, err := NewFallbackSearcher(&stubSearcher{}, provider, enabled).Search(
		t.Context(),
		searchcore.Request{
			Query: "alpha beta gamma", Limit: 10, Verify: searchcore.VerifyIfExist,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != "https://second.example/" {
		t.Fatalf("results = %#v", response.Results)
	}
	if secondCalls.Load() != 1 {
		t.Fatalf("second engine calls = %d, want one", secondCalls.Load())
	}
}

func TestProviderCacheSeparatesRequestVerificationModes(t *testing.T) {
	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (
			*http.Response,
			error,
		) {
			switch request.URL.Host {
			case "first.example":
				firstCalls.Add(1)
			case "second.example":
				secondCalls.Add(1)
			}

			return htmlResponse(http.StatusOK, "answer"), nil
		})},
		Accept:   VerifiedForQuery,
		CacheTTL: time.Minute,
		Now:      fixedClock(),
	})
	provider.engines = []engine{
		tracingEngine(
			"first",
			Result{Title: "unrelated", URL: "https://first.example/"},
		),
		tracingEngine(
			"second",
			Result{Title: "alpha beta", URL: "https://second.example/"},
		),
	}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)
	strict := searchcore.Request{
		Query: "alpha beta gamma", Limit: 10, Verify: searchcore.VerifyIfExist,
	}
	unverified := strict
	unverified.Verify = searchcore.VerifyFalse

	for _, test := range []struct {
		request searchcore.Request
		url     string
	}{
		{request: strict, url: "https://second.example/"},
		{request: unverified, url: "https://first.example/"},
		{request: strict, url: "https://second.example/"},
		{request: unverified, url: "https://first.example/"},
	} {
		response, err := searcher.Search(t.Context(), test.request)
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Results) != 1 || response.Results[0].URL != test.url {
			t.Fatalf("results = %#v, want %q", response.Results, test.url)
		}
	}
	if firstCalls.Load() != 2 || secondCalls.Load() != 1 {
		t.Fatalf(
			"engine calls = first:%d second:%d",
			firstCalls.Load(),
			secondCalls.Load(),
		)
	}
}

func TestProviderCacheIdentityKeepsConstraintListBoundaries(t *testing.T) {
	first := providerRequestCacheIdentity("query", searchcore.Request{
		Terms: []string{"alpha"}, ExcludedTerms: []string{"beta"},
	})
	second := providerRequestCacheIdentity("query", searchcore.Request{
		Terms: []string{"alpha", "beta"},
	})
	if first == second {
		t.Fatalf("cache identities collide: %q", first)
	}
}

func TestProviderTextWithIncludedDomains(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		domains []string
		want    string
	}{
		{name: "none", query: "query", want: "query"},
		{
			name: "one", query: "query", domains: []string{"Example.ORG."},
			want: "query site:example.org",
		},
		{
			name: "alternatives", query: "query",
			domains: []string{"*.one.example", "two.example"},
			want:    "query (site:one.example OR site:two.example)",
		},
		{
			name: "constraint only", domains: []string{"example.org"},
			want: "site:example.org",
		},
		{
			name: "deduplicated and invalid", query: "query",
			domains: []string{
				"example.org", "EXAMPLE.ORG", "", "bad domain", "host:operator",
			},
			want: "query site:example.org",
		},
		{
			name: "ipv6", query: "query", domains: []string{"[2001:db8::1]"},
			want: "query",
		},
		{
			name: "unicode", query: "query", domains: []string{"пример.рф"},
			want: "query site:пример.рф",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := providerTextWithIncludedDomains(test.query, test.domains); got != test.want {
				t.Fatalf("provider text = %q, want %q", got, test.want)
			}
		})
	}
}

func TestProviderTextOmitsUnboundedDomainExpression(t *testing.T) {
	domains := make([]string, 300)
	for index := range domains {
		domains[index] = strings.Repeat("x", 40) + strconv.Itoa(index) + ".example"
	}
	if got := providerTextWithIncludedDomains("query", domains); got != "query" {
		t.Fatalf("provider text = %q, want unscoped bounded query", got)
	}
	if got := providerTextWithIncludedDomains("", domains); got != "" {
		t.Fatalf("constraint-only provider text = %q, want empty bounded query", got)
	}
}

func TestProviderQueryWithoutRequestUsesConfiguredAcceptance(t *testing.T) {
	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (
			*http.Response,
			error,
		) {
			return htmlResponse(http.StatusOK, "answer"), nil
		})},
		Accept: VerifiedForQuery,
		Now:    fixedClock(),
	})
	provider.engines = []engine{
		tracingEngine(
			"first",
			Result{Title: "unrelated", URL: "https://first.example/"},
		),
		tracingEngine(
			"second",
			Result{Title: "needle", URL: "https://second.example/"},
		),
	}

	results, err := provider.Search(context.Background(), "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://second.example/" {
		t.Fatalf("results = %#v", results)
	}
}
