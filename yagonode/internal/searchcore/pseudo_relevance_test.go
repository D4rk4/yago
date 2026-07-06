package searchcore

import (
	"context"
	"errors"
	"testing"
)

type scriptedSearcher struct {
	calls     int
	responses []Response
	errs      []error
}

func (s *scriptedSearcher) Search(context.Context, Request) (Response, error) {
	index := s.calls
	s.calls++
	if index < len(s.errs) && s.errs[index] != nil {
		return Response{}, s.errs[index]
	}
	if index < len(s.responses) {
		return s.responses[index], nil
	}

	return s.responses[len(s.responses)-1], nil
}

func manyResults(n int) []Result {
	out := make([]Result, n)
	for i := range out {
		out[i] = Result{URL: string(rune('a' + i%26))}
	}

	return out
}

func TestPseudoRelevanceExpandsThinRecallQuery(t *testing.T) {
	first := Response{Results: []Result{
		{URL: "1", Title: "alpha", Snippet: "montenegro balkan travel"},
		{URL: "2", Title: "alpha guide", Snippet: "montenegro coast beaches"},
		{URL: "3", Title: "alpha", Snippet: "unrelated words here totally"},
	}}
	second := Response{Results: []Result{
		{URL: "1", Title: "alpha", Snippet: "montenegro balkan travel"},
		{URL: "4", Title: "montenegro facts", Snippet: "expanded recall hit"},
	}}
	inner := &scriptedSearcher{responses: []Response{first, second}}

	resp, err := NewPseudoRelevanceSearcher(inner).Search(
		context.Background(),
		Request{Query: "alpha", Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("expected a second expanded pass, calls = %d", inner.calls)
	}
	found := false
	for _, result := range resp.Results {
		if result.URL == "4" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expanded recall hit missing: %v", urls(resp.Results))
	}
}

func TestPseudoRelevanceSkipsWhenNotHelpful(t *testing.T) {
	// Empty first pass: nothing to mine.
	empty := &scriptedSearcher{responses: []Response{{Results: nil}}}
	if _, err := NewPseudoRelevanceSearcher(empty).Search(
		context.Background(), Request{Query: "alpha", Limit: 10},
	); err != nil || empty.calls != 1 {
		t.Fatalf("empty first pass expanded: calls=%d err=%v", empty.calls, err)
	}

	// A full first page needs no recall help.
	full := &scriptedSearcher{responses: []Response{{Results: manyResults(prfActivateBelow)}}}
	if _, err := NewPseudoRelevanceSearcher(full).Search(
		context.Background(), Request{Query: "alpha", Limit: 10},
	); err != nil || full.calls != 1 {
		t.Fatalf("full page expanded: calls=%d err=%v", full.calls, err)
	}

	// Thin results but no term repeats across feedback docs: no expansion terms.
	noTerms := &scriptedSearcher{responses: []Response{{Results: []Result{
		{URL: "1", Snippet: "aaaa bbbb"},
		{URL: "2", Snippet: "cccc dddd"},
		{URL: "3", Snippet: "eeee ffff"},
	}}}}
	if _, err := NewPseudoRelevanceSearcher(noTerms).Search(
		context.Background(), Request{Query: "alpha", Limit: 10},
	); err != nil || noTerms.calls != 1 {
		t.Fatalf("no-expansion expanded: calls=%d err=%v", noTerms.calls, err)
	}
}

func TestPseudoRelevanceKeepsFirstWhenExpansionFails(t *testing.T) {
	first := Response{Results: []Result{
		{URL: "1", Snippet: "montenegro balkan"},
		{URL: "2", Snippet: "montenegro coast"},
	}}
	inner := &scriptedSearcher{
		responses: []Response{first, {}},
		errs:      []error{nil, errors.New("expanded pass failed")},
	}
	resp, err := NewPseudoRelevanceSearcher(inner).Search(
		context.Background(), Request{Query: "alpha", Limit: 10},
	)
	if err != nil {
		t.Fatalf("expansion failure must not fail the search: %v", err)
	}
	if inner.calls != 2 || len(resp.Results) != 2 {
		t.Fatalf("expected the original two results kept, got %v (calls %d)",
			urls(resp.Results), inner.calls)
	}
}

func TestPseudoRelevanceFirstPassErrorPropagates(t *testing.T) {
	inner := &scriptedSearcher{
		responses: []Response{{}},
		errs:      []error{errors.New("index down")},
	}
	if _, err := NewPseudoRelevanceSearcher(inner).Search(
		context.Background(), Request{Query: "alpha", Limit: 10},
	); err == nil {
		t.Fatal("expected first-pass error to propagate")
	}
}

func TestMinePseudoRelevanceTerms(t *testing.T) {
	results := []Result{
		{Title: "montenegro travel", Snippet: "balkan montenegro coast"},
		{Title: "montenegro guide", Snippet: "balkan beaches"},
		{Title: "other", Snippet: "montenegro balkan"},
	}
	// "montenegro" and "balkan" appear in >=2 docs; the query term and stopwords
	// are excluded; "coast"/"beaches" appear once and drop out.
	got := minePseudoRelevanceTerms(results, []string{"travel"})
	if len(got) != 2 || got[0] != "montenegro" || got[1] != "balkan" {
		t.Fatalf("expansion terms = %v, want [montenegro balkan]", got)
	}

	// The query term is never re-added, short tokens and stopwords are dropped.
	filtered := minePseudoRelevanceTerms([]Result{
		{Snippet: "the and для montenegro"},
		{Snippet: "the montenegro cat"},
	}, []string{"cat"})
	if len(filtered) != 1 || filtered[0] != "montenegro" {
		t.Fatalf("filtered expansion = %v, want [montenegro]", filtered)
	}

	// Higher document frequency ranks ahead of lower, independent of total count.
	byDocFreq := minePseudoRelevanceTerms([]Result{
		{Snippet: "montenegro balkan"},
		{Snippet: "montenegro balkan"},
		{Snippet: "montenegro coast beaches"},
	}, nil)
	if len(byDocFreq) != 2 || byDocFreq[0] != "montenegro" || byDocFreq[1] != "balkan" {
		t.Fatalf("doc-frequency order = %v, want [montenegro balkan]", byDocFreq)
	}

	// The expansion cap bounds how many terms are returned.
	crowded := []Result{
		{Snippet: "aaaa bbbb cccc dddd eeee"},
		{Snippet: "aaaa bbbb cccc dddd eeee"},
	}
	if got := minePseudoRelevanceTerms(crowded, nil); len(got) != prfExpansionTerms {
		t.Fatalf("expansion not capped: %v", got)
	}
}
