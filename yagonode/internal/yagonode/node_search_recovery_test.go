package yagonode

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
)

func fixedCorrector(frequency map[string]int) func() *spellcheck.Corrector {
	corrector := spellcheck.New(frequency)

	return func() *spellcheck.Corrector { return corrector }
}

// scriptedRecoverySearcher answers the exact search with empty results and the
// fuzzy retry with the scripted recovery results.
type scriptedRecoverySearcher struct {
	fuzzyResults []searchcore.Result
	fuzzyErr     error
	requests     []searchcore.Request
	fuzzyCalls   int
}

type deadlineRecoverySearcher struct {
	canceled bool
}

func (s *deadlineRecoverySearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if !req.Fuzzy {
		return searchcore.Response{Request: req}, nil
	}
	<-ctx.Done()
	s.canceled = true

	return searchcore.Response{}, fmt.Errorf("recovery deadline: %w", ctx.Err())
}

func (s *scriptedRecoverySearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.requests = append(s.requests, req)
	if !req.Fuzzy {
		return searchcore.Response{Request: req}, nil
	}
	s.fuzzyCalls++
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
	searcher := withZeroResultRecovery(inner, nil, nil)

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
	resp, err := withZeroResultRecovery(
		empty,
		nil,
		nil,
	).Search(context.Background(), searchcore.Request{
		Query: "void", Terms: []string{"void"},
	})
	if err != nil || resp.Recovered != "" || len(resp.Results) != 0 {
		t.Fatalf("empty fuzzy retry must stay empty: %+v %v", resp, err)
	}

	// No parsed terms: no retry at all.
	noTerms := &scriptedRecoverySearcher{}
	if _, err := withZeroResultRecovery(noTerms, nil, nil).Search(
		context.Background(), searchcore.Request{Query: ""},
	); err != nil || len(noTerms.requests) != 1 {
		t.Fatalf("termless query retried: %d requests", len(noTerms.requests))
	}

	// Already-fuzzy requests never loop.
	fuzzy := &scriptedRecoverySearcher{}
	if _, err := withZeroResultRecovery(fuzzy, nil, nil).Search(
		context.Background(), searchcore.Request{Fuzzy: true, Terms: []string{"x"}},
	); err != nil || len(fuzzy.requests) != 1 {
		t.Fatalf("fuzzy request retried: %d requests", len(fuzzy.requests))
	}

	// A retry error falls back to the honest empty answer.
	failing := &scriptedRecoverySearcher{fuzzyErr: errors.New("boom")}
	resp, err = withZeroResultRecovery(
		failing,
		nil,
		nil,
	).Search(context.Background(), searchcore.Request{
		Query: "void", Terms: []string{"void"},
	})
	if err != nil || resp.Recovered != "" {
		t.Fatalf("failed retry must fall back to the empty answer: %+v %v", resp, err)
	}
}

func TestRecoveryStopsAtItsBudget(t *testing.T) {
	previous := recoverySearchBudget
	recoverySearchBudget = 10 * time.Millisecond
	t.Cleanup(func() { recoverySearchBudget = previous })

	inner := &deadlineRecoverySearcher{}
	response, err := withZeroResultRecovery(inner, inner, nil).Search(
		t.Context(),
		searchcore.Request{Query: "missing", Terms: []string{"missing"}},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !inner.canceled || len(response.Results) != 0 {
		t.Fatalf("budgeted recovery = canceled %v response %#v", inner.canceled, response)
	}
}

func TestRecoverySuggestsFromIndexVocabularyOnTotalMiss(t *testing.T) {
	// The fuzzy retry also finds nothing, but the index-vocabulary corrector
	// still points at the intended spelling.
	miss := &scriptedRecoverySearcher{}
	corrector := fixedCorrector(map[string]int{"golang": 9, "tutorial": 5})
	resp, err := withZeroResultRecovery(miss, nil, corrector).Search(
		context.Background(),
		searchcore.Request{Query: "golnag", Terms: []string{"golnag"}},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 0 || resp.DidYouMean != "golang" {
		t.Fatalf("total miss = results:%d did-you-mean:%q, want a golang suggestion",
			len(resp.Results), resp.DidYouMean)
	}
}

func TestRecoveryPrefersVocabularyCorrectionOverTitles(t *testing.T) {
	// A fuzzy retry surfaces a page, but the vocabulary corrector's suggestion
	// (frequency-ranked over the whole index) wins over the title sample.
	inner := &scriptedRecoverySearcher{fuzzyResults: []searchcore.Result{
		{Title: "unrelated heading", URL: "https://a.example/1"},
	}}
	corrector := fixedCorrector(map[string]int{"golang": 12})
	resp, err := withZeroResultRecovery(inner, nil, corrector).Search(
		context.Background(),
		searchcore.Request{Query: "golnag", Terms: []string{"golnag"}},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if resp.DidYouMean != "golang" {
		t.Fatalf("did you mean = %q, want golang from the vocabulary", resp.DidYouMean)
	}
}

func TestRecoverySuggestsTheReportedRussianCorrection(t *testing.T) {
	inner := &scriptedRecoverySearcher{fuzzyResults: []searchcore.Result{{
		Title: "Психопаты в литературе", URL: "https://a.example/psychology",
	}}}
	response, err := withZeroResultRecovery(inner, nil, nil).Search(
		t.Context(),
		searchcore.Request{Query: "псилобаты", Terms: []string{"псилобаты"}},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if response.Recovered != "fuzzy" || response.DidYouMean != "психопаты" {
		t.Fatalf("recovery = %#v", response)
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

// TestRecoveryRetriesAgainstTheDedicatedSearcher pins PERF-04: the fuzzy retry
// targets the narrow retry searcher (the denylist-filtered local index in the
// assembly), never the full pipeline that already answered empty.
func TestRecoveryRetriesAgainstTheDedicatedSearcher(t *testing.T) {
	inner := &scriptedRecoverySearcher{}
	retry := &scriptedRecoverySearcher{fuzzyResults: []searchcore.Result{
		{URL: "https://local.example/recovered", Title: "Recovered Result"},
	}}
	resp, err := withZeroResultRecovery(inner, retry, nil).Search(
		context.Background(),
		searchcore.Request{Query: "qeury", Terms: []string{"qeury"}},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if resp.Recovered != "fuzzy" || len(resp.Results) != 1 {
		t.Fatalf("recovered = %+v", resp)
	}
	if inner.fuzzyCalls != 0 {
		t.Fatalf("full pipeline retried %d times, want 0 (PERF-04)", inner.fuzzyCalls)
	}
	if retry.fuzzyCalls != 1 {
		t.Fatalf("dedicated retry searcher called %d times, want 1", retry.fuzzyCalls)
	}
}
