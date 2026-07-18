package searchindex

import (
	"sort"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/search"
)

func boundedBodyQueryMatches(
	text string,
	terms search.TermLocationMap,
) []TextQueryMatch {
	matches := make([]TextQueryMatch, 0, min(len(terms), maximumAnalyzedQueryMatches))
	for _, locations := range terms {
		for _, location := range locations {
			match, valid := bodyQueryMatch(text, location)
			if valid {
				matches = append(matches, match)
			}
		}
	}
	sort.Slice(matches, func(left int, right int) bool {
		if matches[left].Start != matches[right].Start {
			return matches[left].Start < matches[right].Start
		}

		return matches[left].End < matches[right].End
	})
	matches = compactBodyQueryMatches(matches)
	if len(matches) <= maximumAnalyzedQueryMatches {
		return matches
	}
	bounded := make([]TextQueryMatch, maximumAnalyzedQueryMatches)
	last := len(matches) - 1
	for index := range bounded {
		bounded[index] = matches[index*last/(maximumAnalyzedQueryMatches-1)]
	}

	return bounded
}

func bodyQueryMatch(text string, location *search.Location) (TextQueryMatch, bool) {
	if location == nil || location.End <= location.Start || location.End > uint64(len(text)) {
		return TextQueryMatch{}, false
	}
	start := len(text[:location.Start])
	end := len(text[:location.End])
	if !utf8.ValidString(text[start:end]) {
		return TextQueryMatch{}, false
	}

	return TextQueryMatch{Start: start, End: end}, true
}

func compactBodyQueryMatches(matches []TextQueryMatch) []TextQueryMatch {
	if len(matches) < 2 {
		return matches
	}
	compacted := matches[:1]
	for _, match := range matches[1:] {
		if match == compacted[len(compacted)-1] {
			continue
		}
		compacted = append(compacted, match)
	}

	return compacted
}
