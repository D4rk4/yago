package tavilyapi

import (
	"strings"
)

const maximumRelevantChunkRunes = 500

// relevantChunks returns up to limit sentences of the text that mention the
// query terms, joined with chunk separators — the chunks_per_source behavior
// for advanced searches. Without matches it falls back to the leading snippet.
func relevantChunks(text string, terms []string, limit int) string {
	if limit <= 0 {
		limit = 1
	}
	chunks := make([]string, 0, limit)
	visitSentences(text, func(sentence string) bool {
		if mentionsAnyTerm(strings.ToLower(sentence), terms) {
			chunks = append(
				chunks,
				strings.Clone(clampRunes(sentence, maximumRelevantChunkRunes)),
			)
		}

		return len(chunks) < limit
	})
	if len(chunks) == 0 {
		return snippet(text)
	}

	return strings.Join(chunks, " [...] ")
}
