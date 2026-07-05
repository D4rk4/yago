// Package snippetmark highlights query terms in result snippets for the human
// search surfaces. The snippet text is HTML-escaped first and only then
// wrapped in <mark> elements, so untrusted page content can never smuggle
// markup, and the output is explicitly marked safe for html/template.
package snippetmark

import (
	"html"
	"html/template"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Highlight returns the snippet with every term occurrence wrapped in
// <mark>, matching case-insensitively on whole-word prefixes so inflected
// forms stay highlighted ("crawl" marks "Crawling"). Terms are matched
// against the raw snippet, and all text is escaped before markup is added.
func Highlight(snippet string, terms []string) template.HTML {
	cleaned := usableTerms(terms)
	if snippet == "" || len(cleaned) == 0 {
		//nolint:gosec // escaped: no markup beyond our own <mark> is emitted.
		return template.HTML(html.EscapeString(snippet)) // nosemgrep
	}

	var out strings.Builder
	lower := strings.ToLower(snippet)
	rest := snippet
	restLower := lower
	for len(rest) > 0 {
		start, length := nextMatch(restLower, rest, cleaned)
		if start < 0 {
			out.WriteString(html.EscapeString(rest))

			break
		}
		out.WriteString(html.EscapeString(rest[:start]))
		out.WriteString("<mark>")
		out.WriteString(html.EscapeString(rest[start : start+length]))
		out.WriteString("</mark>")
		rest = rest[start+length:]
		restLower = restLower[start+length:]
	}

	//nolint:gosec // built exclusively from escaped text and our own <mark>.
	return template.HTML(out.String()) // nosemgrep
}

// nextMatch finds the earliest word-start occurrence of any term and extends
// the match to the end of that word, so the whole inflected form is marked.
func nextMatch(haystackLower string, haystack string, terms []string) (int, int) {
	best, bestLen := -1, 0
	for _, term := range terms {
		offset := 0
		for {
			at := strings.Index(haystackLower[offset:], term)
			if at < 0 {
				break
			}
			at += offset
			if !startsWord(haystack, at) {
				offset = at + len(term)

				continue
			}
			if best < 0 || at < best {
				best, bestLen = at, wordLength(haystack, at, len(term))
			}

			break
		}
	}

	return best, bestLen
}

func startsWord(text string, at int) bool {
	if at == 0 {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(text[:at])

	return !unicode.IsLetter(previous) && !unicode.IsNumber(previous)
}

// wordLength extends a term match to the end of the surrounding word.
func wordLength(text string, at int, termLen int) int {
	end := at + termLen
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			break
		}
		end += size
	}

	return end - at
}

func usableTerms(terms []string) []string {
	cleaned := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term != "" {
			cleaned = append(cleaned, term)
		}
	}

	return cleaned
}
