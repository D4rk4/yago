package searchcore

import (
	"context"
	"errors"
	"math"
	"testing"
)

type stubSearcher struct {
	response Response
	err      error
}

func (s stubSearcher) Search(context.Context, Request) (Response, error) {
	return s.response, s.err
}

func urls(results []Result) []string {
	out := make([]string, len(results))
	for i, result := range results {
		out[i] = result.URL
	}

	return out
}

func TestLexicalRerankLiftsWellMatchedResult(t *testing.T) {
	results := []Result{
		{URL: "a", Score: 1.00, Title: "alpha", Snippet: "foo bar baz qux"},
		{URL: "b", Score: 0.98, Title: "alpha beta", Snippet: "exact adjacent match"},
		{URL: "c", Score: 0.50, Title: "unrelated", Snippet: "nothing here"},
	}
	got := rerankLexicalProximity(results, Request{Terms: []string{"alpha", "beta"}})
	if got[0].URL != "b" {
		t.Fatalf("order = %v, want b lifted to the top", urls(got))
	}
	if len(got) != 3 {
		t.Fatalf("length changed: %v", urls(got))
	}
}

func TestLexicalRerankBreaksScoreTiesByLexical(t *testing.T) {
	results := []Result{
		{URL: "a", Score: 1.0, Title: "alpha", Snippet: "only one term"},
		{URL: "b", Score: 1.0, Title: "alpha beta", Snippet: "both terms adjacent"},
		{URL: "c", Score: 1.0, Title: "alpha zzz zzz beta", Snippet: "both terms apart"},
	}
	got := rerankLexicalProximity(results, Request{Terms: []string{"alpha", "beta"}})
	if urls(got)[0] != "b" || urls(got)[1] != "c" || urls(got)[2] != "a" {
		t.Fatalf("tie-break order = %v, want [b c a]", urls(got))
	}
}

func TestLexicalRerankNoop(t *testing.T) {
	base := []Result{
		{URL: "a", Score: 3, Title: "alpha beta"},
		{URL: "b", Score: 2, Title: "gamma"},
		{URL: "c", Score: 1, Title: "delta"},
	}
	// Single-term query carries no coverage/proximity signal.
	single := rerankLexicalProximity(base, Request{Terms: []string{"alpha"}})
	if urls(single)[0] != "a" || urls(single)[2] != "c" {
		t.Fatalf("single-term reordered: %v", urls(single))
	}
	// Fewer than three results is left untouched.
	short := rerankLexicalProximity(base[:2], Request{Terms: []string{"alpha", "beta"}})
	if len(short) != 2 || urls(short)[0] != "a" {
		t.Fatalf("short list reordered: %v", urls(short))
	}
}

func TestLexicalRerankPreservesTailBeyondWindow(t *testing.T) {
	results := make([]Result, lexicalRerankWindow+2)
	for i := range results {
		results[i] = Result{
			URL:   string(rune('A' + i%26)),
			Score: float64(len(results) - i),
			Title: "no query terms present",
		}
	}
	results[0].URL = "first"
	results[len(results)-1].URL = "last"
	got := rerankLexicalProximity(results, Request{Terms: []string{"alpha", "beta"}})
	if len(got) != len(results) {
		t.Fatalf("length changed: %d", len(got))
	}
	// Descending score with no lexical signal keeps the order, and the tail past
	// the window is copied through untouched.
	if got[0].URL != "first" || got[len(got)-1].URL != "last" {
		t.Fatalf("boundary reordered: first=%s last=%s", got[0].URL, got[len(got)-1].URL)
	}
}

func TestLexicalScore(t *testing.T) {
	terms := []string{"alpha", "beta"}
	cases := map[string]float64{
		"alpha beta":         1.0,  // coverage 1, proximity 1
		"alpha zzz zzz beta": 0.75, // coverage 1, proximity 2/4
		"alpha only here":    0.25, // coverage 1/2, proximity 0
		"":                   0.0,  // nothing matches
		"alpha beta alpha":   1.0,  // duplicate term, still adjacent
		"BETA alpha":         1.0,  // case-folded, order-independent
	}
	for text, want := range cases {
		if got := lexicalScore(text, terms); math.Abs(got-want) > 1e-9 {
			t.Errorf("lexicalScore(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestRerankQueryTerms(t *testing.T) {
	// Parsed terms win, deduped and lowercased, blanks dropped.
	got := rerankQueryTerms(Request{Terms: []string{"Alpha", "alpha", " ", "Beta"}})
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("terms = %v", got)
	}
	// Falls back to splitting the raw query when no parsed terms.
	fallback := rerankQueryTerms(Request{Query: "Gamma delta"})
	if len(fallback) != 2 || fallback[0] != "gamma" || fallback[1] != "delta" {
		t.Fatalf("fallback terms = %v", fallback)
	}
}

func TestLexicalRerankSearcher(t *testing.T) {
	inner := stubSearcher{response: Response{Results: []Result{
		{URL: "a", Score: 1.00, Title: "alpha", Snippet: "one term"},
		{URL: "b", Score: 0.98, Title: "alpha beta", Snippet: "both"},
		{URL: "c", Score: 0.50, Title: "unrelated", Snippet: "none"},
	}}}
	resp, err := NewLexicalRerankSearcher(inner).Search(
		context.Background(),
		Request{Terms: []string{"alpha", "beta"}},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Results[0].URL != "b" {
		t.Fatalf("decorator did not rerank: %v", urls(resp.Results))
	}

	if _, err := NewLexicalRerankSearcher(
		stubSearcher{err: errors.New("backend down")},
	).Search(context.Background(), Request{Query: "alpha beta"}); err == nil {
		t.Fatal("expected inner error to propagate")
	}
}
