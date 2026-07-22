package websearch

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestFallbackDropsUnrelatedProviderResultsAndSeedsOnlyVerified(t *testing.T) {
	provider := &stubProvider{results: []Result{
		{Title: "Православие — энциклопедия", URL: "https://example.org/faith"},
		{Title: "YouTube", URL: "https://www.youtube.com/", Snippet: "Share your videos"},
	}}
	seeder := &stubSeeder{done: make(chan struct{})}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled, WithSeeder(seeder))

	resp, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "православие", Terms: []string{"православие"}, Limit: 10},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.org/faith" {
		t.Fatalf("results = %#v", resp.Results)
	}
	if resp.TotalResults != 1 {
		t.Fatalf("total = %d, want 1", resp.TotalResults)
	}
	select {
	case <-seeder.done:
	case <-time.After(time.Second):
		t.Fatal("verified result seeding did not run")
	}
	if seeder.calls != 1 || len(seeder.urls) != 1 || seeder.urls[0] != "https://example.org/faith" {
		t.Fatalf("seeder = %#v", seeder)
	}
}

func TestVerifiedWebResultsTrustsWhenVerifyFalse(t *testing.T) {
	results := []Result{{Title: "unrelated", URL: "https://example.org/"}}
	kept := verifiedWebResults(
		searchcore.Request{Query: "gap", Verify: searchcore.VerifyFalse},
		results,
	)
	if len(kept) != 1 {
		t.Fatalf("verify=false dropped rows: %#v", kept)
	}
}

func TestResultsMentioningTermsRequireBoundedDistinctCoverage(t *testing.T) {
	results := []Result{
		{Title: "alpha only", URL: "https://weak.example/"},
		{Title: "alpha beta", URL: "https://first.example/"},
		{Snippet: "beta gamma", URL: "https://second.example/"},
		{Title: "unrelated", URL: "https://other.example/"},
	}

	got := resultsMentioningTerms([]string{"alpha", "ALPHA", "beta", "gamma"}, results)
	if len(got) != 2 || got[0].URL != "https://first.example/" ||
		got[1].URL != "https://second.example/" {
		t.Fatalf("results = %#v", got)
	}
}

func TestResultsMentioningTermsRequireBothTermsInTwoTermQuery(t *testing.T) {
	results := []Result{
		{Title: "alpha only", URL: "https://weak.example/"},
		{Title: "alpha beta", URL: "https://strong.example/"},
	}

	got := resultsMentioningTerms([]string{"alpha", "beta"}, results)
	if len(got) != 1 || got[0].URL != "https://strong.example/" {
		t.Fatalf("results = %#v", got)
	}
}

func TestResultsMentioningTermsKeepAmbiguousCrossLanguageWords(t *testing.T) {
	results := []Result{
		{Title: "Spyder history", URL: "https://weak.example/"},
		{Title: "Can-Am Spyder guide", URL: "https://strong.example/"},
	}

	got := resultsMentioningTerms([]string{"can", "am", "spyder"}, results)
	if len(got) != 1 || got[0].URL != "https://strong.example/" {
		t.Fatalf("results = %#v", got)
	}
}

func TestResultsMentioningTermsAcceptTwoAmbiguousWordsWhenBothMatch(t *testing.T) {
	results := []Result{{Title: "Can Spyder owners guide", URL: "https://strong.example/"}}

	got := resultsMentioningTerms([]string{"can", "spyder"}, results)
	if len(got) != 1 || got[0].URL != "https://strong.example/" {
		t.Fatalf("results = %#v", got)
	}
}

func TestResultsMentioningTermsRetainOrdinaryFunctionWordRequirements(t *testing.T) {
	results := []Result{
		{Title: "Weather forecast", URL: "https://weak.example/"},
		{Title: "What is weather forecast", URL: "https://strong.example/"},
	}

	got := resultsMentioningTerms([]string{"what", "is", "the", "weather"}, results)
	if len(got) != 1 || got[0].URL != "https://strong.example/" {
		t.Fatalf("results = %#v", got)
	}
}

func TestVerifiedWebResultsEnforcesStructuredConstraints(t *testing.T) {
	tests := []struct {
		name     string
		request  searchcore.Request
		accepted Result
		rejected Result
	}{
		{
			name: "site host", request: searchcore.Request{SiteHost: "example.org"},
			accepted: Result{URL: "https://www.example.org/guide.pdf"},
			rejected: Result{URL: "https://docs.example.org/guide.pdf"},
		},
		{
			name: "www site host", request: searchcore.Request{SiteHost: "www.example.org"},
			accepted: Result{URL: "https://example.org/guide.pdf"},
			rejected: Result{URL: "https://www.docs.example.org/guide.pdf"},
		},
		{
			name: "top level domain", request: searchcore.Request{TLD: ".org"},
			accepted: Result{URL: "https://example.org/guide.pdf"},
			rejected: Result{URL: "https://example.com/guide.pdf"},
		},
		{
			name: "file type", request: searchcore.Request{FileType: ".PDF"},
			accepted: Result{URL: "https://example.org/guide.pdf?download=1"},
			rejected: Result{URL: "https://example.org/guide.html"},
		},
		{
			name: "url text", request: searchcore.Request{InURL: "GUIDE"},
			accepted: Result{URL: "https://example.org/guide.pdf"},
			rejected: Result{URL: "https://example.org/reference.pdf"},
		},
		{
			name: "excluded term", request: searchcore.Request{ExcludedTerms: []string{"java"}},
			accepted: Result{Title: "Golang tools", URL: "https://example.org/tools.pdf"},
			rejected: Result{Title: "Golang and Java tools", URL: "https://example.org/tools.pdf"},
		},
		{
			name:     "included parent domain",
			request:  searchcore.Request{IncludeDomains: []string{"example.org"}},
			accepted: Result{URL: "https://docs.example.org/guide.pdf"},
			rejected: Result{URL: "https://example.net/guide.pdf"},
		},
		{
			name:     "excluded parent domain",
			request:  searchcore.Request{ExcludeDomains: []string{"blocked.example"}},
			accepted: Result{URL: "https://allowed.example/guide.pdf"},
			rejected: Result{URL: "https://deep.blocked.example/guide.pdf"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.request.Verify = searchcore.VerifyFalse
			got := verifiedWebResults(test.request, []Result{test.accepted, test.rejected})
			if len(got) != 1 || got[0] != test.accepted {
				t.Fatalf("results = %#v", got)
			}
		})
	}
}

func TestVerifiedWebResultsRejectsMalformedConstrainedURL(t *testing.T) {
	got := verifiedWebResults(searchcore.Request{
		SiteHost: "example.org", Verify: searchcore.VerifyFalse,
	}, []Result{{URL: "://invalid"}})
	if len(got) != 0 {
		t.Fatalf("results = %#v", got)
	}
}

func TestVerifiedForQueryParsesPositiveQuotedTerms(t *testing.T) {
	results := []Result{
		{Title: "Mind the gap", URL: "https://example.org/gap"},
		{Title: "Rail fares", URL: "https://example.org/fares"},
		{Title: "Unrelated", URL: "https://example.org/other"},
	}

	kept := VerifiedForQuery(
		`site:example.org "mind the gap" -"rail fares"`,
		results,
	)
	if len(kept) != 1 || kept[0].URL != "https://example.org/gap" {
		t.Fatalf("verified quoted results = %#v", kept)
	}
}

func TestFallbackCascadeAcceptsRawQuotedQuery(t *testing.T) {
	provider := &stubProvider{results: []Result{
		{Title: "Mind the gap", URL: "https://example.org/gap"},
		{Title: "Rail fares", URL: "https://example.org/fares"},
	}}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)

	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: `site:example.org "mind the gap" -"rail fares"`, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || len(response.Results) != 1 ||
		response.Results[0].URL != "https://example.org/gap" {
		t.Fatalf("response = %#v provider calls = %d", response, provider.calls)
	}
}
