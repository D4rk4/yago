package snippetfetch

import "strings"

// excerptRuneCap matches the local index's snippet length so peer and local
// rows read alike.
const excerptRuneCap = 320

// queryBiasedExcerpt centers a snippet-length window of the text on the
// earliest content-term occurrence, with a little lead so the sentence context
// survives (Tombros & Sanderson, SIGIR 1998 — query-biased summaries let the
// searcher judge relevance without opening the page).
func queryBiasedExcerpt(text string, terms []string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= excerptRuneCap {
		return text
	}
	folded := strings.ToLower(text)
	anchor := -1
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if at := strings.Index(folded, term); at >= 0 && (anchor < 0 || at < anchor) {
			anchor = at
		}
	}
	if anchor < 0 {
		return strings.TrimSpace(string(runes[:excerptRuneCap]))
	}
	runeAnchor := len([]rune(folded[:anchor]))
	start := max(runeAnchor-excerptRuneCap/4, 0)
	end := min(start+excerptRuneCap, len(runes))
	excerpt := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		excerpt = "… " + excerpt
	}

	return excerpt
}
