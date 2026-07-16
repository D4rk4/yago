// Package snippetmark highlights query terms in result snippets for the human
// search surfaces. The snippet text is HTML-escaped first and only then
// wrapped in <mark> elements, so untrusted page content can never smuggle
// markup, and the output is explicitly marked safe for html/template.
package snippetmark

import (
	"html"
	"html/template"
	"strings"
)

func HighlightMatches(
	snippet string,
	terms []string,
	analyzed []QueryMatch,
) template.HTML {
	cleaned := usableTerms(terms)
	matches := mergedQueryMatches(snippet, cleaned, analyzed)
	if snippet == "" || len(matches) == 0 {
		//nolint:gosec // escaped: no markup beyond our own <mark> is emitted.
		return template.HTML(html.EscapeString(snippet)) // nosemgrep
	}

	var out strings.Builder
	cursor := 0
	for _, match := range matches {
		out.WriteString(html.EscapeString(snippet[cursor:match.Start]))
		out.WriteString("<mark>")
		out.WriteString(html.EscapeString(snippet[match.Start:match.End]))
		out.WriteString("</mark>")
		cursor = match.End
	}
	out.WriteString(html.EscapeString(snippet[cursor:]))

	//nolint:gosec // built exclusively from escaped text and our own <mark>.
	return template.HTML(out.String()) // nosemgrep
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
