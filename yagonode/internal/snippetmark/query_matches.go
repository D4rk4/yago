package snippetmark

import (
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/querymatch"
)

type QueryMatch struct {
	Start int
	End   int
}

func mergedQueryMatches(
	text string,
	terms []string,
	analyzed []QueryMatch,
) []QueryMatch {
	matches := validAnalyzedMatches(text, analyzed)
	if analyzed == nil {
		matches = appendTokenQueryMatches(matches, text, terms)
	}

	return coalescedQueryMatches(matches)
}

func appendTokenQueryMatches(
	matches []QueryMatch,
	text string,
	terms []string,
) []QueryMatch {
	matches = appendBoundedTermQueryMatches(matches, text, terms)
	for offset := 0; offset < len(text); {
		start, end, found := nextQueryWord(text, offset)
		if !found {
			break
		}
		if tokenMatchesAnyTerm(text[start:end], terms) {
			matches = append(matches, QueryMatch{Start: start, End: end})
		} else {
			matches = appendIntraTokenQueryMatches(matches, text[start:end], start, terms)
		}
		offset = end
	}

	return matches
}

func appendBoundedTermQueryMatches(
	matches []QueryMatch,
	text string,
	terms []string,
) []QueryMatch {
	for _, term := range terms {
		if !querymatch.TermContainsWordSeparator(term) {
			continue
		}
		for offset := 0; offset <= len(text); {
			start, end, found := querymatch.NextBoundedTerm(text, term, offset)
			if !found {
				break
			}
			matches = append(matches, QueryMatch{Start: start, End: end})
			offset = end
		}
	}

	return matches
}

func nextQueryWord(text string, offset int) (int, int, bool) {
	for offset < len(text) {
		current, width := utf8.DecodeRuneInString(text[offset:])
		if wordRune(current) {
			end := offset + width
			for end < len(text) {
				current, width = utf8.DecodeRuneInString(text[end:])
				if !wordRune(current) {
					break
				}
				end += width
			}

			return offset, end, true
		}
		offset += width
	}

	return 0, 0, false
}

func tokenMatchesAnyTerm(token string, terms []string) bool {
	for _, term := range terms {
		if !querymatch.TermContainsWordSeparator(term) &&
			!querymatch.TermCanMatchWithinToken(term) &&
			querymatch.TokenMatchesTerm(token, term) {
			return true
		}
	}

	return false
}

func appendIntraTokenQueryMatches(
	matches []QueryMatch,
	token string,
	start int,
	terms []string,
) []QueryMatch {
	for _, term := range terms {
		if querymatch.TermContainsWordSeparator(term) ||
			!querymatch.TermCanMatchWithinToken(term) {
			continue
		}
		for offset := 0; offset < len(token); {
			matchStart, matchEnd, found := querymatch.NextLiteralTerm(token, term, offset)
			if !found {
				break
			}
			matches = append(matches, QueryMatch{
				Start: start + matchStart,
				End:   start + matchEnd,
			})
			offset = matchEnd
		}
	}

	return matches
}

func wordRune(current rune) bool {
	return unicode.IsLetter(current) || unicode.IsNumber(current) || unicode.IsMark(current)
}

func coalescedQueryMatches(matches []QueryMatch) []QueryMatch {
	if len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(left, right int) bool {
		if matches[left].Start != matches[right].Start {
			return matches[left].Start < matches[right].Start
		}

		return matches[left].End > matches[right].End
	})
	merged := matches[:0]
	for _, match := range matches {
		last := len(merged) - 1
		if last >= 0 && match.Start < merged[last].End {
			merged[last].End = max(merged[last].End, match.End)

			continue
		}
		merged = append(merged, match)
	}

	return merged
}

func validAnalyzedMatches(text string, analyzed []QueryMatch) []QueryMatch {
	if analyzed == nil {
		return nil
	}
	valid := make([]QueryMatch, 0, len(analyzed))
	for _, match := range analyzed {
		if match.Start < 0 || match.End <= match.Start || match.End > len(text) ||
			!utf8.ValidString(text[match.Start:match.End]) {
			continue
		}
		valid = append(valid, match)
	}

	return valid
}
