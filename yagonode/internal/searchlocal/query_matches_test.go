package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestCoreQueryMatchesMapsAnalyzerOffsets(t *testing.T) {
	result := searchindex.SearchResult{
		Snippet:  "чрезвычайных полномочий путину",
		Analyzer: "ru",
	}
	query := searchindex.NewAnalyzedQueryTerms(
		[]string{"чрезвычайные", "полномочия", "путина"},
		result.Analyzer,
	)
	matches := coreAnalyzedQueryMatches(result.Snippet, query)
	if len(matches) != 3 ||
		result.Snippet[matches[0].Start:matches[0].End] != "чрезвычайных" ||
		result.Snippet[matches[1].Start:matches[1].End] != "полномочий" ||
		result.Snippet[matches[2].Start:matches[2].End] != "путину" {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestCoreQueryMatchesPreservesAuthoritativeEmptyEvidence(t *testing.T) {
	result := searchindex.SearchResult{Snippet: "spaceship", Analyzer: "en"}
	query := searchindex.NewAnalyzedQueryTerms([]string{"space"}, result.Analyzer)
	matches := coreAnalyzedQueryMatches(result.Snippet, query)
	if matches == nil || len(matches) != 0 {
		t.Fatalf("matches = %#v, want non-nil empty evidence", matches)
	}
}
