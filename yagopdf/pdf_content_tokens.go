package yagopdf

import "bytes"

type pdfContentTokenKind uint8

const (
	pdfContentNumber pdfContentTokenKind = iota + 1
	pdfContentString
	pdfContentName
	pdfContentArrayStart
	pdfContentArrayEnd
	pdfContentArray
	pdfContentDictionary
	pdfContentOperator
)

type pdfContentToken struct {
	kind   pdfContentTokenKind
	number float64
	value  []byte
	name   string
}

type pdfContentScanner struct {
	content []byte
	at      int
}

func (s *pdfContentScanner) next(stringLimit int) (pdfContentToken, bool) {
	for s.at < len(s.content) {
		current := s.content[s.at]
		switch {
		case pdfWhiteSpace(current):
			s.at++
		case current == '%':
			s.at = pdfLineEnd(s.content, s.at+1)
		case current == '(':
			raw, end := pdfRawStringLiteralWithin(s.content[s.at:], stringLimit)
			s.at += end + 1

			return pdfContentToken{kind: pdfContentString, value: raw}, true
		case current == '<':
			return s.angleBracketToken(stringLimit), true
		case current == '[':
			s.at++

			return pdfContentToken{kind: pdfContentArrayStart}, true
		case current == ']':
			s.at++

			return pdfContentToken{kind: pdfContentArrayEnd}, true
		case current == '/':
			name, end, valid := pdfDecodedNameToken(s.content[s.at:], pdfMaxPDFNameBytes)
			s.at += end
			if !valid {
				return pdfContentToken{kind: pdfContentDictionary}, true
			}

			return pdfContentToken{kind: pdfContentName, name: name}, true
		case pdfNameDelimiter(current):
			s.at++
		case current == '+' || current == '-' || current == '.' || current >= '0' && current <= '9':
			return s.numberToken(), true
		default:
			return s.operatorToken(), true
		}
	}

	return pdfContentToken{}, false
}

func (s *pdfContentScanner) angleBracketToken(stringLimit int) pdfContentToken {
	rest := s.content[s.at:]
	if len(rest) > 1 && rest[1] == '<' {
		length := pdfDictionaryLength(rest)
		if length == 0 {
			length = len(rest)
		}
		s.at += length

		return pdfContentToken{kind: pdfContentDictionary}
	}
	raw, end := pdfRawHexStringWithin(rest, stringLimit)
	s.at += end + 1

	return pdfContentToken{kind: pdfContentString, value: raw}
}

func (s *pdfContentScanner) numberToken() pdfContentToken {
	start := s.at
	for s.at < len(s.content) &&
		!pdfWhiteSpace(s.content[s.at]) && !pdfNameDelimiter(s.content[s.at]) {
		s.at++
	}
	value, valid := pdfContentNumberValue(s.content[start:s.at])
	if !valid {
		return pdfContentToken{kind: pdfContentDictionary}
	}
	return pdfContentToken{kind: pdfContentNumber, number: value}
}

func (s *pdfContentScanner) operatorToken() pdfContentToken {
	start := s.at
	for s.at < len(s.content) &&
		!pdfWhiteSpace(s.content[s.at]) && !pdfNameDelimiter(s.content[s.at]) {
		s.at++
	}
	keyword := s.content[start:s.at]
	if string(keyword) == "ID" {
		s.skipInlineImageData()
	}

	return pdfContentToken{kind: pdfContentOperator, value: keyword}
}

func (s *pdfContentScanner) skipInlineImageData() {
	s.at++
	for s.at < len(s.content) {
		index := bytes.Index(s.content[s.at:], []byte("EI"))
		if index < 0 {
			s.at = len(s.content)

			return
		}
		end := s.at + index
		s.at = end + len("EI")
		if pdfWhiteSpace(s.content[end-1]) &&
			(s.at == len(s.content) || pdfWhiteSpace(s.content[s.at])) {
			return
		}
	}
}
