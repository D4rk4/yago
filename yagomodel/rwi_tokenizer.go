package yagomodel

import (
	"strings"
	"unicode"
)

const MinimumRWIWordLength = 2

func TokenizeRWIText(text string, maximum int) []string {
	tokenizer := rwiTokenizer{
		runes:   []rune(text),
		maximum: maximum,
		words:   make([]string, 0),
	}
	tokenizer.scan()
	return tokenizer.words
}

type rwiTokenizer struct {
	runes             []rune
	maximum           int
	words             []string
	word              []rune
	wasDigitSeparator bool
}

func (t *rwiTokenizer) scan() {
	for index, current := range t.runes {
		if t.appendHyphen(index, current) || t.appendDigitSeparator(index, current) {
			continue
		}
		t.clearDigitSeparatorBeforeLetter(current)
		if t.consumeOrdinary(index, current) {
			return
		}
	}
	t.flush()
}

func (t *rwiTokenizer) appendHyphen(index int, current rune) bool {
	if current != '-' || index >= len(t.runes)-1 ||
		(!unicode.IsLetter(t.runes[index+1]) && !unicode.IsDigit(t.runes[index+1])) {
		return false
	}
	t.word = append(t.word, current)
	return true
}

func (t *rwiTokenizer) appendDigitSeparator(index int, current rune) bool {
	if (current != '.' && current != ',') || index == 0 || index >= len(t.runes)-1 ||
		!unicode.IsDigit(t.runes[index-1]) || !unicode.IsDigit(t.runes[index+1]) {
		return false
	}
	t.word = append(t.word, current)
	t.wasDigitSeparator = true
	return true
}

func (t *rwiTokenizer) clearDigitSeparatorBeforeLetter(current rune) {
	if t.wasDigitSeparator && unicode.IsLetter(current) {
		t.wasDigitSeparator = false
	}
}

func (t *rwiTokenizer) consumeOrdinary(index int, current rune) bool {
	switch {
	case isRWIPunctuation(current):
		return t.consumePunctuation(current)
	case isRWISeparator(current):
		return t.flushAndClearDigitSeparator()
	default:
		return t.consumeWordRune(index, current)
	}
}

func (t *rwiTokenizer) consumePunctuation(current rune) bool {
	if len(t.word) > 0 && !t.wasDigitSeparator && t.flush() {
		return true
	}
	t.word = append(t.word, current)
	if t.flush() {
		return true
	}
	t.wasDigitSeparator = false
	return false
}

func (t *rwiTokenizer) flushAndClearDigitSeparator() bool {
	if t.flush() {
		return true
	}
	t.wasDigitSeparator = false
	return false
}

func (t *rwiTokenizer) consumeWordRune(index int, current rune) bool {
	t.word = append(t.word, current)
	if index < len(t.runes)-1 && unicode.IsDigit(current) &&
		unicode.IsLetter(t.runes[index+1]) {
		return t.flush()
	}
	return false
}

func (t *rwiTokenizer) flush() bool {
	if len(t.word) >= MinimumRWIWordLength {
		t.words = append(t.words, strings.ToLower(string(t.word)))
	}
	t.word = nil
	return t.maximum > 0 && len(t.words) == t.maximum
}

func isRWIPunctuation(value rune) bool {
	return value == '.' || value == '!' || value == '?'
}

func isRWISeparator(value rune) bool {
	return !unicode.IsLetter(value) && !unicode.IsDigit(value) && value != '.' && value != ','
}
