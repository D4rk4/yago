package searchindex

import (
	"unicode"
	"unicode/utf8"
)

func boundedSnippet(text string, witness TextQueryEvidence, fallback string) string {
	start, end := boundedSnippetWindow(text, witness)

	return normalizedSnippetWindow(text[start:end], start > 0, fallback)
}

func sentenceSnippet(witness snippetWitness, fallback string) string {
	text := witness.text
	start, end := boundedSnippetWindow(text, witness.evidence)
	start = alignedSentenceStart(text, start, witness.evidence.Start)
	end = alignedSentenceEnd(text, witness.evidence.End, end)

	return normalizedSnippetWindow(text[start:end], start > 0, fallback)
}

func boundedSnippetWindow(text string, witness TextQueryEvidence) (int, int) {
	start := max(
		offsetRunesBefore(text, witness.Start, snippetRuneCap/4),
		min(witness.Start, offsetRunesBefore(text, witness.End, snippetRuneCap)),
	)
	end := offsetRunesAfter(text, start, snippetRuneCap)
	start = offsetRunesBefore(
		text,
		start,
		snippetRuneCap-utf8.RuneCountInString(text[start:end]),
	)

	return start, end
}

func alignedSentenceStart(text string, start int, witnessStart int) int {
	if start == 0 {
		return 0
	}
	_, size := utf8.DecodeLastRuneInString(text[:start])
	from := start - size
	for offset, character := range text[from:witnessStart] {
		if sentenceBoundaryRune(text, from+offset, character) {
			return from + offset + utf8.RuneLen(character)
		}
	}

	return start
}

func alignedSentenceEnd(text string, witnessEnd int, end int) int {
	if end == len(text) {
		return end
	}
	_, size := utf8.DecodeRuneInString(text[end:])
	from := min(witnessEnd, end)
	aligned := end
	for offset, character := range text[from : end+size] {
		if sentenceBoundaryRune(text, from+offset, character) {
			aligned = from + offset
		}
	}

	return aligned
}

func sentenceBoundaryRune(text string, offset int, character rune) bool {
	if !unicode.IsSpace(character) {
		return false
	}
	if character == '\n' {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(text[:offset])

	return unicode.Is(unicode.Sentence_Terminal, previous)
}

func offsetRunesBefore(text string, offset int, runes int) int {
	for ; offset > 0 && runes > 0; runes-- {
		_, size := utf8.DecodeLastRuneInString(text[:offset])
		offset -= size
	}

	return offset
}

func offsetRunesAfter(text string, offset int, runes int) int {
	for ; offset < len(text) && runes > 0; runes-- {
		_, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
	}

	return offset
}
