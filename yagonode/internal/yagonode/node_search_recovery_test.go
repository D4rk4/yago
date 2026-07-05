package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// scriptedRecoverySearcher answers the exact search with empty results and the
// fuzzy retry with the scripted recovery results.
type scriptedRecoverySearcher struct {
	fuzzyResults []searchcore.Result
	fuzzyErr     error
	requests     []searchcore.Request
}

func (s *scriptedRecoverySearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.requests = append(s.requests, req)
	if !req.Fuzzy {
		return searchcore.Response{Request: req}, nil
	}
	if s.fuzzyErr != nil {
		return searchcore.Response{}, s.fuzzyErr
	}

	return searchcore.Response{
		Request:      req,
		TotalResults: len(s.fuzzyResults),
		Results:      s.fuzzyResults,
	}, nil
}

func TestRecoveryRetriesFuzzyAndSuggestsSpelling(t *testing.T) {
	inner := &scriptedRecoverySearcher{fuzzyResults: []searchcore.Result{
		{Title: "Golang Tutorial", URL: "https://a.example/1"},
	}}
	searcher := withZeroResultRecovery(inner)

	resp, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "golnag tutorial",
		Terms: []string{"golnag", "tutorial"},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if resp.Recovered != "fuzzy" || len(resp.Results) != 1 {
		t.Fatalf(
			"recovered = %q results=%d, want fuzzy recovery",
			resp.Recovered,
			len(resp.Results),
		)
	}
	if resp.DidYouMean != "golang tutorial" {
		t.Fatalf("did you mean = %q, want %q", resp.DidYouMean, "golang tutorial")
	}
	if resp.Request.Fuzzy {
		t.Fatal("recovered response must carry the original (non-fuzzy) request")
	}
	if len(inner.requests) != 2 || !inner.requests[1].Fuzzy {
		t.Fatalf("requests = %+v, want exact then fuzzy", inner.requests)
	}
}

func TestRecoveryKeepsHonestEmptyAndSkipsWhenNotEligible(t *testing.T) {
	empty := &scriptedRecoverySearcher{}
	resp, err := withZeroResultRecovery(empty).Search(context.Background(), searchcore.Request{
		Query: "void", Terms: []string{"void"},
	})
	if err != nil || resp.Recovered != "" || len(resp.Results) != 0 {
		t.Fatalf("empty fuzzy retry must stay empty: %+v %v", resp, err)
	}

	// No parsed terms: no retry at all.
	noTerms := &scriptedRecoverySearcher{}
	if _, err := withZeroResultRecovery(noTerms).Search(
		context.Background(), searchcore.Request{Query: ""},
	); err != nil || len(noTerms.requests) != 1 {
		t.Fatalf("termless query retried: %d requests", len(noTerms.requests))
	}

	// Already-fuzzy requests never loop.
	fuzzy := &scriptedRecoverySearcher{}
	if _, err := withZeroResultRecovery(fuzzy).Search(
		context.Background(), searchcore.Request{Fuzzy: true, Terms: []string{"x"}},
	); err != nil || len(fuzzy.requests) != 1 {
		t.Fatalf("fuzzy request retried: %d requests", len(fuzzy.requests))
	}

	// A retry error falls back to the honest empty answer.
	failing := &scriptedRecoverySearcher{fuzzyErr: errors.New("boom")}
	resp, err = withZeroResultRecovery(failing).Search(context.Background(), searchcore.Request{
		Query: "void", Terms: []string{"void"},
	})
	if err != nil || resp.Recovered != "" {
		t.Fatalf("failed retry must fall back to the empty answer: %+v %v", resp, err)
	}
}

func TestDidYouMeanOnlySuggestsRealCorrections(t *testing.T) {
	results := []searchcore.Result{{Title: "Golang Weekly — Issue 42"}}
	if got := didYouMean([]string{"golang"}, results); got != "" {
		t.Fatalf("exact term corrected: %q", got)
	}
	if got := didYouMean([]string{"go"}, results); got != "" {
		t.Fatalf("short term corrected: %q", got)
	}
	if got := didYouMean([]string{"golnag", "weekly"}, results); got != "golang weekly" {
		t.Fatalf("correction = %q, want %q", got, "golang weekly")
	}
}

func TestTitleWordsSamplesOnlyTheFirstTitles(t *testing.T) {
	results := make([]searchcore.Result, didYouMeanTitleSample+1)
	for i := range results {
		results[i] = searchcore.Result{Title: "filler words"}
	}
	results[didYouMeanTitleSample] = searchcore.Result{Title: "golang"}

	if got := didYouMean([]string{"golnag"}, results); got != "" {
		t.Fatalf("vocabulary leaked past the title sample: %q", got)
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"golnag", "golang", 2},
		{"kitten", "sitting", 3},
		{"привет", "привед", 1},
	}
	for _, tc := range cases {
		if got := editDistance(tc.a, tc.b); got != tc.want {
			t.Fatalf("editDistance(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
