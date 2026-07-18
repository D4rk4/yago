package searchlocal

import (
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func coreBodyQueryMatches(matches []searchindex.TextQueryMatch) []searchcore.QueryMatch {
	if matches == nil {
		return nil
	}
	mapped := make([]searchcore.QueryMatch, len(matches))
	for index, match := range matches {
		mapped[index] = searchcore.QueryMatch{Start: match.Start, End: match.End}
	}

	return mapped
}
