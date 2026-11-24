package searchindex

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/queryidentifier"
)

func documentHasIdentifier(doc documentstore.Document, identifier queryidentifier.Hyphenated) bool {
	if textHasIdentifier(doc.Title, identifier) || textHasIdentifier(documentURL(doc), identifier) {
		return true
	}
	for _, heading := range doc.Headings {
		if textHasIdentifier(heading, identifier) {
			return true
		}
	}
	for _, anchor := range anchorTexts(doc.Inlinks) {
		if textHasIdentifier(anchor, identifier) {
			return true
		}
	}

	return textHasIdentifier(doc.ExtractedText, identifier)
}

func textHasIdentifier(text string, identifier queryidentifier.Hyphenated) bool {
	firstEnd := -1
	for start, end := range rangeStoredTokens(text) {
		if firstEnd >= 0 && strings.EqualFold(text[start:end], identifier.Last) {
			dash, width := utf8.DecodeRuneInString(text[firstEnd:start])
			if firstEnd+width == start && unicode.Is(unicode.Dash, dash) {
				return true
			}
		}
		firstEnd = -1
		if strings.EqualFold(text[start:end], identifier.First) {
			firstEnd = end
		}
	}

	return false
}
