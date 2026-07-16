package yagonode

import (
	"html/template"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetmark"
)

func highlightedResultSnippet(
	result searchcore.Result,
	terms []string,
) template.HTML {
	var matches []snippetmark.QueryMatch
	if result.QueryMatches != nil {
		matches = make([]snippetmark.QueryMatch, len(result.QueryMatches))
		for index, match := range result.QueryMatches {
			matches[index] = snippetmark.QueryMatch{Start: match.Start, End: match.End}
		}
	}

	return snippetmark.HighlightMatches(result.Snippet, terms, matches)
}
