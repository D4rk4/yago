package tavilyapi

import (
	"strings"
	"unicode"
)

const (
	answerBasicRuneCap    = 300
	answerAdvancedRuneCap = 600
	answerMaxSentences    = 4
	answerTopResults      = 5
)

// extractiveAnswer synthesizes the include_answer text locally: sentences from
// the top result snippets that carry the query's terms, stitched in rank order
// and bounded by the mode's length. No model is involved — this is extractive
// summarization over what the search itself returned; an empty result set
// yields an empty answer, never an invented one.
func extractiveAnswer(mode inclusionMode, query string, results []SearchResult) string {
	limit := answerBasicRuneCap
	if strings.EqualFold(string(mode), "advanced") {
		limit = answerAdvancedRuneCap
	}
	builder := &answerBuilder{
		terms: answerTerms(query),
		limit: limit,
		seen:  map[string]bool{},
	}
	for index, result := range results {
		if index >= answerTopResults || builder.full() {
			break
		}
		builder.consume(result.Content)
	}

	return clampRunes(strings.TrimSpace(builder.out.String()), limit)
}

// answerBuilder accumulates matching sentences under the answer budget.
type answerBuilder struct {
	out       strings.Builder
	terms     []string
	limit     int
	seen      map[string]bool
	sentences int
}

func (b *answerBuilder) full() bool { return b.sentences >= answerMaxSentences }

// consume appends the snippet's qualifying sentences until the budget binds.
func (b *answerBuilder) consume(content string) {
	for _, sentence := range splitSentences(content) {
		if b.full() {
			return
		}
		normalized := strings.ToLower(sentence)
		if b.seen[normalized] || !mentionsAnyTerm(normalized, b.terms) {
			continue
		}
		if b.out.Len() > 0 && len([]rune(b.out.String()+" "+sentence)) > b.limit {
			continue
		}
		b.seen[normalized] = true
		if b.out.Len() > 0 {
			b.out.WriteByte(' ')
		}
		b.out.WriteString(sentence)
		b.sentences++
	}
}

// answerTerms lowers the query into match terms, dropping operators.
func answerTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.Contains(field, ":") || strings.HasPrefix(field, "-") {
			continue
		}
		terms = append(terms, strings.Trim(field, `"'`))
	}

	return terms
}

func mentionsAnyTerm(sentence string, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	for _, term := range terms {
		if term != "" && strings.Contains(sentence, term) {
			return true
		}
	}

	return false
}

// splitSentences breaks snippet text on sentence punctuation, keeping the
// terminator with the sentence.
func splitSentences(text string) []string {
	sentences := make([]string, 0, 8)
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			if sentence := strings.TrimSpace(current.String()); sentenceWorthKeeping(sentence) {
				sentences = append(sentences, sentence)
			}
			current.Reset()
		}
	}
	if sentence := strings.TrimSpace(current.String()); sentenceWorthKeeping(sentence) {
		sentences = append(sentences, sentence)
	}

	return sentences
}

// sentenceWorthKeeping drops fragments too short to inform an answer.
func sentenceWorthKeeping(sentence string) bool {
	letters := 0
	for _, r := range sentence {
		if unicode.IsLetter(r) {
			letters++
		}
	}

	return letters >= 12
}

func clampRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return strings.TrimSpace(string(runes[:limit]))
}
