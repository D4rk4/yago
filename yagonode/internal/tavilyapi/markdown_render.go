package tavilyapi

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// documentMarkdown renders best-effort markdown from a stored document: the
// title becomes the top heading, extracted-text lines that match the page's
// recorded headings render as second-level headings, existing list markers
// survive, and paragraphs separate with blank lines. Structure that HTML
// extraction flattened (tables, nesting) cannot be recovered.
func documentMarkdown(doc documentstore.Document) string {
	var out strings.Builder
	if doc.Title != "" {
		out.WriteString("# " + doc.Title + "\n")
	}
	headings := make(map[string]bool, len(doc.Headings))
	for _, heading := range doc.Headings {
		if trimmed := strings.TrimSpace(heading); trimmed != "" {
			headings[trimmed] = true
		}
	}
	for _, line := range strings.Split(doc.ExtractedText, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case headings[line]:
			out.WriteString("\n## " + line + "\n")
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			out.WriteString(line + "\n")
		default:
			out.WriteString("\n" + line + "\n")
		}
	}

	return strings.TrimSpace(out.String())
}

// relevantChunks returns up to limit sentences of the text that mention the
// query terms, joined with chunk separators — the chunks_per_source behavior
// for advanced searches. Without matches it falls back to the leading snippet.
func relevantChunks(text string, terms []string, limit int) string {
	if limit <= 0 {
		limit = 1
	}
	chunks := make([]string, 0, limit)
	for _, sentence := range splitSentences(text) {
		if len(chunks) >= limit {
			break
		}
		if mentionsAnyTerm(strings.ToLower(sentence), terms) {
			chunks = append(chunks, sentence)
		}
	}
	if len(chunks) == 0 {
		return snippet(text)
	}

	return strings.Join(chunks, " [...] ")
}
