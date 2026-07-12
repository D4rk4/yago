package searchindex

import (
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

// queryBiasedSnippet centers the snippet on the query: the window of the text
// covering the densest cluster of query-term occurrences serves as the
// excerpt (with a leading ellipsis when it starts mid-text); a text without
// any term falls back to the leading snippet. Query-biased excerpts let the
// searcher judge relevance without opening the page (Tombros & Sanderson,
// SIGIR 1998).
func queryBiasedSnippet(text string, terms []string, fallback string) string {
	return queryBiasedSnippetWithEvidence(text, terms, "", fallback)
}

func queryBiasedSnippetWithEvidence(
	text string,
	terms []string,
	evidence string,
	fallback string,
) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	if textWithinRuneLimit(text, snippetRuneCap) || len(terms) == 0 {
		return snippet(text, fallback)
	}
	// Anchor on a content word: a function word («что», "the") occurs earlier
	// in almost any text and would center every snippet on it, hiding the
	// passage that actually answers the query.
	anchorTerms := stopwords.ContentTerms(terms)
	if len(anchorTerms) == 0 {
		anchorTerms = terms
	}
	anchor := -1
	if evidence != "" {
		anchor = strings.Index(text, evidence)
	}
	if anchor < 0 {
		anchor = firstTextTermAnchor(text, anchorTerms)
	}
	if anchor < 0 {
		anchor = firstTextTermAnchor(text, terms)
	}
	if anchor < 0 {
		return snippet(text, fallback)
	}

	return snippetAtByte(text, anchor, fallback)
}

func snippetAtByte(text string, anchor int, fallback string) string {
	anchor = min(max(anchor, 0), len(text))
	start := anchor
	for count := 0; start > 0 && count < snippetRuneCap/4; count++ {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		start -= size
	}
	end := start
	for count := 0; end < len(text) && count < snippetRuneCap; count++ {
		_, size := utf8.DecodeRuneInString(text[end:])
		end += size
	}

	return normalizedSnippetWindow(text[start:end], start > 0, fallback)
}

func textWithinRuneLimit(text string, limit int) bool {
	count := 0
	for range text {
		count++
		if count > limit {
			return false
		}
	}

	return true
}

func normalizedSnippetWindow(text string, ellipsis bool, fallback string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return fallback
	}
	if ellipsis {
		return "… " + text
	}

	return text
}

func firstTextTermAnchor(text string, terms []string) int {
	return newSnippetTermAnchorScan(text, terms).first()
}

func tokenMatchesAnyTerm(token string, terms []string) bool {
	for _, term := range terms {
		if strings.EqualFold(token, strings.TrimSpace(term)) {
			return true
		}
	}

	return false
}
