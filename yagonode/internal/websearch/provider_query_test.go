package websearch

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestFallbackNormalizesCompoundDashForProvider(t *testing.T) {
	provider := &stubProvider{results: []Result{{
		Title: "Гном Гномыч рассказал о тренировках",
		URL:   "https://example.org/gnom-gnomych",
	}}}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)

	response, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "гном-гномыч", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.gotQuery != "гном гномыч" {
		t.Fatalf("provider query = %q, want normalized words", provider.gotQuery)
	}
	if len(response.Results) != 1 || response.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf("response results = %#v", response.Results)
	}
}

func TestParallelNormalizesCompoundDashBesidePrimaryHit(t *testing.T) {
	primary := &stubSearcher{resp: searchcore.Response{
		Results:      []searchcore.Result{{Title: "Local", URL: "https://local.example/"}},
		TotalResults: 1,
	}}
	provider := &stubProvider{results: []Result{{
		Title: "Гном Гномыч рассказал о тренировках",
		URL:   "https://example.org/gnom-gnomych",
	}}}
	searcher := NewParallelSearcher(primary, provider, enabled)

	response, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "гном-гномыч", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.gotQuery != "гном гномыч" {
		t.Fatalf("provider query = %q, want normalized words", provider.gotQuery)
	}
	foundWeb := false
	for _, result := range response.Results {
		foundWeb = foundWeb || result.Source == searchcore.SourceWeb
	}
	if !foundWeb {
		t.Fatalf("parallel results = %#v", response.Results)
	}
}

func TestParallelDDGSNormalizesAndCachesCompoundQuery(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (
		*http.Response,
		error,
	) {
		calls.Add(1)
		if got := request.URL.Query().Get("q"); got != "гном гномыч" {
			t.Fatalf("provider query = %q", got)
		}

		return htmlResponse(http.StatusOK, `<!doctype html><html><body>
<div class="result"><a class="result__a" href="https://example.org/gnom">Гном Гномыч</a>
<span class="result__snippet">Новости Гном Гномыча</span></div>
</body></html>`), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendAuto, CacheTTL: time.Minute,
		Now: fixedClock(), Accept: VerifiedForQuery,
	})
	primary := &stubSearcher{resp: searchcore.Response{
		Results:      []searchcore.Result{{Title: "Local", URL: "https://local.example/"}},
		TotalResults: 1,
	}}
	searcher := NewParallelSearcher(primary, provider, enabled)

	for _, query := range []string{"гном-гномыч", "гном гномыч"} {
		response, err := searcher.Search(context.Background(), searchcore.Request{
			Query: query, Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		foundWeb := false
		for _, result := range response.Results {
			foundWeb = foundWeb || result.Source == searchcore.SourceWeb
		}
		if !foundWeb {
			t.Fatalf("query %q results = %#v", query, response.Results)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want one normalized cache entry", got)
	}
}

func TestDDGSVerificationKeepsCompoundOperatorLexical(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (
		*http.Response,
		error,
	) {
		if got := request.URL.Query().Get("q"); got != "near death" {
			t.Fatalf("provider query = %q", got)
		}

		return htmlResponse(http.StatusOK, `<!doctype html><html><body>
<div class="result"><a class="result__a" href="https://example.org/near">Near death approach</a></div>
</body></html>`), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendDuckDuckGo, Now: fixedClock(),
		Accept: VerifiedForQuery,
	})

	results, err := provider.Search(t.Context(), "near-death", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://example.org/near" {
		t.Fatalf("results = %#v", results)
	}
}
