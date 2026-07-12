package searchindex

import (
	"strings"
	"unicode"
)

type snippetTermAnchorScan struct {
	text       string
	terms      []string
	anchor     int
	tokenStart int
}

func newSnippetTermAnchorScan(text string, terms []string) *snippetTermAnchorScan {
	return &snippetTermAnchorScan{
		text:       text,
		terms:      terms,
		anchor:     firstLiteralTermAnchor(text, terms),
		tokenStart: -1,
	}
}

func firstLiteralTermAnchor(text string, terms []string) int {
	anchor := -1
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		index := strings.Index(text, term)
		if index >= 0 && (anchor < 0 || index < anchor) {
			anchor = index
		}
	}

	return anchor
}

func (s *snippetTermAnchorScan) first() int {
	for index, character := range s.text {
		if s.advance(index, character) {
			return s.anchor
		}
	}
	s.finish(len(s.text))

	return s.anchor
}

func (s *snippetTermAnchorScan) advance(index int, character rune) bool {
	if unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsMark(character) {
		if s.tokenStart < 0 {
			s.tokenStart = index
		}

		return false
	}
	s.finish(index)

	return s.anchor >= 0 && index > s.anchor
}

func (s *snippetTermAnchorScan) finish(end int) {
	if s.tokenStart >= 0 && tokenMatchesAnyTerm(s.text[s.tokenStart:end], s.terms) &&
		(s.anchor < 0 || s.tokenStart < s.anchor) {
		s.anchor = s.tokenStart
	}
	s.tokenStart = -1
}
