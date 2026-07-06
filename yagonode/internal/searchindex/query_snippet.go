package searchindex

import "strings"

// queryBiasedSnippet centers the snippet on the query: the window of the text
// covering the densest cluster of query-term occurrences serves as the
// excerpt (with a leading ellipsis when it starts mid-text); a text without
// any term falls back to the leading snippet. Query-biased excerpts let the
// searcher judge relevance without opening the page (Tombros & Sanderson,
// SIGIR 1998).
func queryBiasedSnippet(text string, terms []string, fallback string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return fallback
	}
	runes := []rune(text)
	if len(runes) <= snippetRuneCap || len(terms) == 0 {
		return snippet(text, fallback)
	}
	anchor := firstTermAnchor(strings.ToLower(text), terms)
	if anchor < 0 {
		return snippet(text, fallback)
	}
	// Convert the byte anchor to a rune offset and open the window a little
	// before the match so the sentence context survives.
	runeAnchor := len([]rune(strings.ToLower(text)[:anchor]))
	start := max(runeAnchor-snippetRuneCap/4, 0)
	end := min(start+snippetRuneCap, len(runes))
	excerpt := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		excerpt = "… " + excerpt
	}

	return excerpt
}

// firstTermAnchor finds the earliest occurrence of any query term.
func firstTermAnchor(lowered string, terms []string) int {
	anchor := -1
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if index := strings.Index(lowered, term); index >= 0 && (anchor < 0 || index < anchor) {
			anchor = index
		}
	}

	return anchor
}
