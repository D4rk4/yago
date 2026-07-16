package searchcore

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/querymatch"
)

type lexicalLiteralSpan struct {
	start int
	end   int
	term  string
}

func lexicalTextTermPositions(text string, terms []string) map[string][]int {
	positions := make(map[string][]int, len(terms))
	spans := lexicalQueryLiteralSpans(text, terms)
	spanIndex := 0
	ordinal := 0
	for offset := 0; offset < len(text); {
		if spanIndex < len(spans) && spans[spanIndex].start == offset {
			span := spans[spanIndex]
			positions[span.term] = append(positions[span.term], ordinal)
			spanIndex++
			ordinal++
			offset = span.end

			continue
		}
		current, width := utf8.DecodeRuneInString(text[offset:])
		if !lexicalWordRune(current) {
			offset += width

			continue
		}
		end := offset + width
		for end < len(text) && (spanIndex >= len(spans) || end < spans[spanIndex].start) {
			current, width = utf8.DecodeRuneInString(text[end:])
			if !lexicalWordRune(current) {
				break
			}
			end += width
		}
		if term, found := bestTokenTerm(text[offset:end], terms); found {
			positions[term] = append(positions[term], ordinal)
		}
		ordinal++
		offset = end
	}

	return positions
}

func lexicalQueryLiteralSpans(text string, terms []string) []lexicalLiteralSpan {
	candidates := make([]lexicalLiteralSpan, 0)
	for _, term := range terms {
		var next func(string, string, int) (int, int, bool)
		switch {
		case querymatch.TermContainsWordSeparator(term):
			next = querymatch.NextBoundedTerm
		case querymatch.TermCanMatchWithinToken(term):
			next = querymatch.NextLiteralTerm
		default:
			continue
		}
		for offset := 0; offset <= len(text); {
			start, end, found := next(text, term, offset)
			if !found {
				break
			}
			candidates = append(candidates, lexicalLiteralSpan{
				start: start,
				end:   end,
				term:  term,
			})
			offset = end
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].start != candidates[right].start {
			return candidates[left].start < candidates[right].start
		}

		return candidates[left].end > candidates[right].end
	})
	selected := candidates[:0]
	for _, candidate := range candidates {
		if len(selected) > 0 && candidate.start < selected[len(selected)-1].end {
			continue
		}
		selected = append(selected, candidate)
	}

	return selected
}

func bestTokenTerm(token string, terms []string) (string, bool) {
	token = strings.ToLower(token)
	for _, term := range terms {
		if querymatch.TermContainsWordSeparator(term) ||
			querymatch.TermCanMatchWithinToken(term) {
			continue
		}
		if token == strings.ToLower(strings.TrimSpace(term)) {
			return term, true
		}
	}
	for _, term := range terms {
		if querymatch.TermContainsWordSeparator(term) ||
			querymatch.TermCanMatchWithinToken(term) {
			continue
		}
		if TokenMatchesTerm(token, term) {
			return term, true
		}
	}

	return "", false
}

func lexicalWordRune(current rune) bool {
	return unicode.IsLetter(current) || unicode.IsNumber(current) || unicode.IsMark(current)
}
