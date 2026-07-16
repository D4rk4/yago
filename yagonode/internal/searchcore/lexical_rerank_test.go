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

type capturingRankingSearcher struct {
	got      Request
	response Response
}

func (s *capturingRankingSearcher) Search(
	_ context.Context,
	req Request,
) (Response, error) {
	s.got = req

	return s.response, nil
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
	for signal, want := range map[RankingSignal]float64{
		SignalTermCoverage:    1,
		SignalGlobalProximity: 1,
	} {
		if value, known := got[0].Evidence.Value(signal); !known || value != want {
			t.Fatalf("evidence %s = %v/%v, want %v", signal.Name(), value, known, want)
		}
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

func TestLexicalRerankUsesWordFormsForPeerAndWebRows(t *testing.T) {
	results := []Result{
		{
			URL: "peer", Score: 1, Source: SourceRemote,
			Snippet: "Чрезвычайных полномочий передали Путину",
		},
		{
			URL: "web", Score: 0.9, Source: SourceWeb,
			Snippet: "Чрезвычайных полномочий передали Путину",
		},
		{URL: "unrelated", Score: 0.8, Source: SourceLocal, Snippet: "Другой текст"},
	}
	got := rerankLexicalProximity(results, Request{
		Terms: []string{"чрезвычайные", "полномочия", "путина"},
	})
	for _, result := range got[:2] {
		coverage, coverageKnown := result.Evidence.Value(SignalTermCoverage)
		proximity, proximityKnown := result.Evidence.Value(SignalGlobalProximity)
		if !coverageKnown || coverage != 1 || !proximityKnown || proximity != 0.75 {
			t.Fatalf("federated evidence for %s = %v/%v %v/%v", result.URL,
				coverage, coverageKnown, proximity, proximityKnown)
		}
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

func TestLexicalScoreMatchesRussianInflections(t *testing.T) {
	coverage, proximity := lexicalTextComponents(
		"Чрезвычайных полномочий Путину.",
		[]string{"чрезвычайные", "полномочия", "путина"},
	)
	if coverage != 1 || proximity != 1 {
		t.Fatalf("Russian morphology coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalScoreDoesNotDoubleCountOneMorphologicalOccurrence(t *testing.T) {
	coverage, proximity := lexicalTextComponents("games", []string{"game", "games"})
	if coverage != 0.5 || proximity != 0 {
		t.Fatalf("shared occurrence coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalTextComponentsEmptyTerms(t *testing.T) {
	coverage, proximity := lexicalTextComponents("text", nil)
	if coverage != 0 || proximity != 0 {
		t.Fatalf("empty coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalTextComponentsKeepCombiningMarksInsideTokens(t *testing.T) {
	coverage, proximity := lexicalTextComponents("שָׁלוֹם עולם", []string{"שָׁלוֹם"})
	if coverage != 1 || proximity != 0 {
		t.Fatalf("combining-mark coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalTextComponentsMatchUnsegmentedScriptSubstring(t *testing.T) {
	coverage, proximity := lexicalTextComponents("東京タワー", []string{"東京"})
	if coverage != 1 || proximity != 0 {
		t.Fatalf("unsegmented-script coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalTextComponentsRequireCompletePunctuatedIdentifier(t *testing.T) {
	coverage, proximity := lexicalTextComponents(
		"Use Node.js guide",
		[]string{"node.js", "guide"},
	)
	if coverage != 1 || proximity != 1 {
		t.Fatalf("identifier coverage/proximity = %v/%v", coverage, proximity)
	}
	coverage, proximity = lexicalTextComponents("node server and node.jsp", []string{"node.js"})
	if coverage != 0 || proximity != 0 {
		t.Fatalf("partial identifier coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalTextComponentsDoNotReuseIdentifierComponent(t *testing.T) {
	coverage, proximity := lexicalTextComponents("node.js", []string{"node.js", "node"})
	if coverage != 0.5 || proximity != 0 {
		t.Fatalf("identifier component coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalTextComponentsUseDistinctUnsegmentedSpans(t *testing.T) {
	coverage, proximity := lexicalTextComponents(
		"東京タワー",
		[]string{"東京", "タワー"},
	)
	if coverage != 1 || proximity != 1 {
		t.Fatalf("unsegmented spans coverage/proximity = %v/%v", coverage, proximity)
	}
}

func TestLexicalQueryLiteralSpansPreferLongestNonOverlappingWitness(t *testing.T) {
	text := "東京タワー"
	spans := lexicalQueryLiteralSpans(text, []string{"東京", "東京タワー", "タワー"})
	if len(spans) != 1 || spans[0].start != 0 || spans[0].end != len(text) ||
		spans[0].term != "東京タワー" {
		t.Fatalf("literal spans = %#v", spans)
	}
}

func TestLexicalTextComponentsRejectUnsegmentedPrefixAffinity(t *testing.T) {
	coverage, proximity := lexicalTextComponents("東京都庁内", []string{"東京都庁舎"})
	if coverage != 0 || proximity != 0 {
		t.Fatalf("unsegmented prefix coverage/proximity = %v/%v", coverage, proximity)
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

func TestLexicalRerankSearcherOwnsCandidateAndResultWindows(t *testing.T) {
	inner := &capturingRankingSearcher{response: Response{Results: []Result{
		{URL: "a", Score: 3},
		{URL: "b", Score: 2},
		{URL: "c", Score: 1},
	}}}
	resp, err := NewLexicalRerankSearcher(inner).Search(
		t.Context(),
		Request{Query: "alpha", Limit: 1},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if inner.got.Offset != 0 || inner.got.Limit != 50 || !inner.got.RankingFeatures {
		t.Fatalf("candidate request = %+v", inner.got)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "a" || resp.Request.Limit != 1 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestRankingCandidateRequest(t *testing.T) {
	cases := []struct {
		req  Request
		want int
	}{
		{Request{}, 50},
		{Request{Limit: 11}, 55},
		{Request{Limit: 20}, 100},
		{Request{Limit: 10, Offset: 90}, 100},
		{Request{Limit: 10, Offset: 100}, 110},
	}
	for _, tc := range cases {
		got := rankingCandidateRequest(tc.req)
		if got.Offset != 0 || got.Limit != tc.want || !got.RankingFeatures {
			t.Errorf("rankingCandidateRequest(%+v) = %+v, want limit %d", tc.req, got, tc.want)
		}
	}
}

func TestLexicalRerankSearcherPreservesDateOrder(t *testing.T) {
	inner := stubSearcher{response: Response{Results: []Result{
		{URL: "old", Score: 1, Title: "alpha beta", Date: "20200101"},
		{URL: "middle", Score: 0.9, Title: "alpha beta", Date: "20240101"},
		{URL: "new", Score: 0.8, Title: "alpha beta", Date: "20260101"},
	}}}
	resp, err := NewLexicalRerankSearcher(inner).Search(
		t.Context(),
		Request{Terms: []string{"alpha", "beta"}, SortByDate: true},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := urls(resp.Results); got[0] != "new" || got[1] != "middle" || got[2] != "old" {
		t.Fatalf("date order = %v", got)
	}
}

func TestLexicalRerankSearcherAppliesFinalHostCrowding(t *testing.T) {
	inner := stubSearcher{response: Response{Results: []Result{
		{URL: "a1", Host: "a.example", Score: 1, Title: "alpha beta"},
		{URL: "a2", Host: "a.example", Score: 0.99, Title: "alpha beta"},
		{URL: "a3", Host: "a.example", Score: 0.98, Title: "alpha beta"},
		{URL: "b1", Host: "b.example", Score: 0.1, Title: "other result"},
	}}}
	resp, err := NewLexicalRerankSearcher(inner).Search(
		t.Context(),
		Request{Terms: []string{"alpha", "beta"}},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := urls(resp.Results); got[2] != "b1" || got[3] != "a3" {
		t.Fatalf("crowded order = %v", got)
	}
}

func TestRerankQueryTermsAllStopwordFallback(t *testing.T) {
	got := rerankQueryTerms(Request{Terms: []string{"что", "и", "the"}})
	if len(got) != 3 {
		t.Fatalf("all-stopword query terms = %#v", got)
	}
	got = rerankQueryTerms(Request{Terms: []string{"что", "осень", "осень"}})
	if len(got) != 1 || got[0] != "осень" {
		t.Fatalf("content terms = %#v", got)
	}
}
