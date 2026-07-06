package pageindex

import (
	"strings"
	"unicode"
)

// minWordLength mirrors YaCy's Tokenizer.wordminsize: shorter tokens are noise, not indexed.
const minWordLength = 2

// startingPhraseNumber mirrors YaCy's Tokenizer, which numbers body sentences from 100 upward.
const startingPhraseNumber = 100

type wordOccurrence struct {
	firstPosition         int
	count                 int
	firstPhraseNumber     int
	firstPositionInPhrase int
}

type textStatistics struct {
	Words   int
	Phrases int
}

func tokenize(
	text string,
) (order []string, occurrences map[string]wordOccurrence, stats textStatistics) {
	occurrences = map[string]wordOccurrence{}
	runes := []rune(text)
	total := 0
	var builder strings.Builder
	phraseNumber := startingPhraseNumber
	positionInPhrase := 1
	wordSeenInPhrase := false

	emit := func() {
		if builder.Len() == 0 {
			return
		}
		word := builder.String()
		builder.Reset()
		if len([]rune(word)) < minWordLength {
			return
		}
		existing, seen := occurrences[word]
		if !seen {
			order = append(order, word)
			occurrences[word] = wordOccurrence{
				firstPosition:         total,
				count:                 1,
				firstPhraseNumber:     phraseNumber,
				firstPositionInPhrase: positionInPhrase,
			}
		} else {
			existing.count++
			occurrences[word] = existing
		}
		total++
		positionInPhrase++
		wordSeenInPhrase = true
	}

	// A sentence only counts as a phrase if it held at least one indexable word (YaCy skips runs of bare punctuation).
	endPhrase := func() {
		if wordSeenInPhrase {
			phraseNumber++
		}
		positionInPhrase = 1
		wordSeenInPhrase = false
	}

	for i, r := range runes {
		switch {
		// A hyphen glued to a following letter/digit stays inside the word, e.g. "state-of-the-art".
		case r == '-' && i+1 < len(runes) && isWordRune(runes[i+1]):
			builder.WriteRune(r)
		// '.' or ',' between two digits is a number separator, not a word boundary, e.g. "3.14" or "1,234".
		case isDigitSeparator(r) && i > 0 && unicode.IsDigit(runes[i-1]) && i+1 < len(runes) && unicode.IsDigit(runes[i+1]):
			builder.WriteRune(r)
		case isWordRune(r):
			builder.WriteRune(unicode.ToLower(r))
		case isSentenceEnd(r):
			emit()
			endPhrase()
		default:
			emit()
		}
	}
	emit()
	// A trailing sentence with no closing punctuation still counts as a phrase if it held words.
	if wordSeenInPhrase {
		phraseNumber++
	}
	return order, occurrences, textStatistics{
		Words:   total,
		Phrases: phraseNumber - startingPhraseNumber,
	}
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isDigitSeparator(r rune) bool {
	return r == '.' || r == ','
}

func isSentenceEnd(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}
