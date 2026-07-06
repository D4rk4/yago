package websearch

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestFallbackDropsUnrelatedProviderResultsAndSeedsOnlyVerified(t *testing.T) {
	provider := &stubProvider{results: []Result{
		{Title: "Православие — энциклопедия", URL: "https://example.org/faith"},
		{Title: "YouTube", URL: "https://www.youtube.com/", Snippet: "Share your videos"},
	}}
	seeder := &stubSeeder{}
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
