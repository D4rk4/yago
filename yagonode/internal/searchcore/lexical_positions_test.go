package searchcore

import (
	"math"
	"testing"
)

func bodyPositions(terms map[string][]int) map[string]map[string][]int {
	return map[string]map[string][]int{"body": terms}
}

func TestLexicalScoreFromPositions(t *testing.T) {
	terms := []string{"alpha", "beta"}
	cases := []struct {
		name   string
		fields map[string]map[string][]int
		want   float64
		ok     bool
	}{
		{"empty", nil, 0, false},
		{"adjacent", bodyPositions(map[string][]int{"alpha": {1}, "beta": {2}}), 1.0, true},
		{"apart", bodyPositions(map[string][]int{"alpha": {1}, "beta": {5}}), 0.7, true},
		{"one term", bodyPositions(map[string][]int{"alpha": {1}}), 0.25, true},
		{"only stemmed keys", bodyPositions(map[string][]int{"gamma": {1}}), 0, false},
	}
	for _, tc := range cases {
		got, ok := lexicalScoreFromPositions(tc.fields, terms)
		if ok != tc.ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: got (%v,%v), want (%v,%v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLexicalScoreFromPositionsTakesBestField(t *testing.T) {
	// alpha/beta are adjacent in the title but far apart in the body; the tightest
	// single-field window wins while coverage stays one across the fields.
	fields := map[string]map[string][]int{
		"title": {"alpha": {1}, "beta": {2}},
		"body":  {"alpha": {1}, "beta": {40}},
	}
	got, ok := lexicalScoreFromPositions(fields, []string{"alpha", "beta"})
	if !ok || math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("best-field score = (%v,%v), want (1,true)", got, ok)
	}
}

func TestLexicalSignalPrefersPositionsOverSnippet(t *testing.T) {
	terms := []string{"alpha", "beta"}
	// The snippet scatters the terms (snippet score 0.75) but the document
	// positions put them adjacent, so the signal follows the positions.
	withPositions := Result{
		Title:              "",
		Snippet:            "alpha zzz zzz beta",
		FieldTermPositions: bodyPositions(map[string][]int{"alpha": {1}, "beta": {2}}),
	}
	if got := lexicalSignal(withPositions, terms); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("positions signal = %v, want 1.0", got)
	}
	// No positions: fall back to the title-plus-snippet text.
	snippetOnly := Result{Snippet: "alpha beta"}
	if got := lexicalSignal(snippetOnly, terms); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("snippet fallback signal = %v, want 1.0", got)
	}
}

func TestLexicalRerankUsesDocumentPositions(t *testing.T) {
	// b's snippet does not carry the terms, but its document positions place them
	// adjacent, so the document-scoped signal lifts it over a snippet-only tie.
	// The third result only pads the list past the window minimum of three.
	results := []Result{
		{URL: "a", Score: 1.0, Title: "alpha", Snippet: "alpha only, beta missing here"},
		{
			URL: "b", Score: 0.99, Title: "unrelated heading", Snippet: "no query terms visible",
			FieldTermPositions: bodyPositions(map[string][]int{"alpha": {4}, "beta": {5}}),
		},
		{URL: "c", Score: 0.5, Title: "filler"},
	}
	got := rerankLexicalProximity(results, Request{Terms: []string{"alpha", "beta"}})
	if got[0].URL != "b" {
		t.Fatalf("order = %v, want b lifted by its document positions", urls(got))
	}
}
