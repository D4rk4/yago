package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2/analysis"
)

// offsetAnalyzer emits the offsets it is told to, including the impossible ones
// a char-filtering or dictionary analyzer can produce when its token offsets no
// longer describe the input it was handed.
type offsetAnalyzer struct {
	tokens analysis.TokenStream
}

func (a offsetAnalyzer) Analyze([]byte) analysis.TokenStream {
	return a.tokens
}

// Every published match must be a slice the caller can take. TextQueryMatch
// offsets are byte offsets into the exact text that was analyzed, and callers —
// the passage builder, the snippet evidence, the RSS highlighter — index the
// string with them directly. An analyzer whose token offsets do not describe the
// text it was given would turn a highlight into an out-of-range slice, so the
// matcher drops such a token instead of publishing it. The in-range token beside
// it must still be published, proving the guard rejects tokens rather than
// abandoning the whole text.
func TestTextQueryMatchesDropsTokensOutsideTheAnalyzedText(t *testing.T) {
	text := "term"
	query := AnalyzedQueryTerms{
		analyzer:  offsetAnalyzer{tokens: outOfRangeQueryTokens(text)},
		targets:   map[string]struct{}{"term": {}},
		available: true,
	}
	matches := query.TextMatches(text)
	if len(matches) != 1 || matches[0] != (TextQueryMatch{Start: 0, End: len(text)}) {
		t.Fatalf("matches = %#v, want only the in-range token", matches)
	}
	for _, match := range matches {
		if match.Start < 0 || match.End <= match.Start || match.End > len(text) {
			t.Fatalf("published unusable match %#v for %d-byte text", match, len(text))
		}
		_ = text[match.Start:match.End]
	}
}

func outOfRangeQueryTokens(text string) analysis.TokenStream {
	return analysis.TokenStream{
		{Term: []byte("term"), Start: -1, End: 2},
		{Term: []byte("term"), Start: 2, End: 2},
		{Term: []byte("term"), Start: 3, End: 1},
		{Term: []byte("term"), Start: 0, End: len(text) + 1},
		{Term: []byte("term"), Start: 0, End: len(text)},
	}
}
