package searchlocal

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type requestSnippetMatches struct {
	terms      []string
	byAnalyzer map[string]searchindex.AnalyzedQueryTerms
}

func newRequestSnippetMatches(req searchcore.Request) *requestSnippetMatches {
	terms := req.Terms
	if len(terms) == 0 {
		terms = strings.Fields(req.Query)
	}

	return &requestSnippetMatches{
		terms:      terms,
		byAnalyzer: make(map[string]searchindex.AnalyzedQueryTerms),
	}
}

func (matches *requestSnippetMatches) result(
	result searchindex.SearchResult,
) []searchcore.QueryMatch {
	query, found := matches.byAnalyzer[result.Analyzer]
	if !found {
		query = searchindex.NewAnalyzedQueryTerms(matches.terms, result.Analyzer)
		matches.byAnalyzer[result.Analyzer] = query
	}

	return coreAnalyzedQueryMatches(result.Snippet, query)
}

func coreAnalyzedQueryMatches(
	snippet string,
	query searchindex.AnalyzedQueryTerms,
) []searchcore.QueryMatch {
	matches := query.TextMatches(snippet)
	if matches == nil {
		return nil
	}
	mapped := make([]searchcore.QueryMatch, len(matches))
	for index, match := range matches {
		mapped[index] = searchcore.QueryMatch{Start: match.Start, End: match.End}
	}

	return mapped
}
