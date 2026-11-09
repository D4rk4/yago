package yagopdf

import "bytes"

const pdfMaxStructureDepth = 64

type pdfDictionaryCursor struct {
	value []byte
	at    int
}

func newPDFDictionaryCursor(value []byte) pdfDictionaryCursor {
	at := pdfSkipSpace(value, 0)
	if !bytes.HasPrefix(value[at:], []byte("<<")) {
		return pdfDictionaryCursor{}
	}

	return pdfDictionaryCursor{value: value, at: at + 2}
}

func (c *pdfDictionaryCursor) next() (string, []byte, bool) {
	for c.at < len(c.value) {
		c.at = pdfSkipSpace(c.value, c.at)
		if c.at >= len(c.value) || c.value[c.at] == '>' {
			break
		}
		if c.value[c.at] != '/' {
			c.at = pdfValueEnd(c.value, c.at)

			continue
		}
		name, length, _ := pdfDecodedNameToken(c.value[c.at:], pdfMaxPDFNameBytes)
		start := pdfSkipSpace(c.value, c.at+length)
		c.at = pdfValueEnd(c.value, start)
		if c.at <= start {
			break
		}

		return name, c.value[start:c.at], true
	}

	return "", nil, false
}

func pdfDictionaryEntryValue(dictionary []byte, name string) []byte {
	cursor := newPDFDictionaryCursor(dictionary)
	for {
		entry, value, exists := cursor.next()
		if !exists {
			return nil
		}
		if entry == name {
			return value
		}
	}
}

func pdfSkipSpace(value []byte, at int) int {
	for at < len(value) {
		switch {
		case pdfWhiteSpace(value[at]):
			at++
		case value[at] == '%':
			at = pdfLineEnd(value, at+1)
		default:
			return at
		}
	}

	return at
}

func pdfValueEnd(value []byte, at int) int {
	if at >= len(value) {
		return len(value)
	}
	if value[at] == '[' || bytes.HasPrefix(value[at:], []byte("<<")) {
		return pdfCompositeEnd(value, at)
	}
	if _, end := pdfReferenceAt(value, at); end > at {
		return end
	}

	return pdfTokenEnd(value, at)
}

func pdfTokenEnd(value []byte, at int) int {
	if at >= len(value) {
		return len(value)
	}
	switch value[at] {
	case '(':
		_, consumed := pdfRawStringLiteralWithin(value[at:], 0)

		return min(len(value), at+consumed+1)
	case '<':
		return pdfHexEnd(value, at+1)
	case '/':
		at++
	case '[', ']', '>', ')', '{', '}':
		return at + 1
	}
	if end := bytes.IndexAny(value[at:], "\x00\t\n\f\r ()<>[]{}/%{}"); end >= 0 {
		return at + end
	}

	return len(value)
}

func pdfCompositeEnd(value []byte, start int) int {
	depth := 0
	for at := start; at < len(value); {
		at = pdfSkipSpace(value, at)
		if at >= len(value) {
			break
		}
		switch {
		case value[at] == '[':
			depth++
			at++
		case bytes.HasPrefix(value[at:], []byte("<<")):
			depth++
			at += 2
		case value[at] == ']':
			depth--
			at++
		case bytes.HasPrefix(value[at:], []byte(">>")):
			depth--
			at += 2
		default:
			at = pdfTokenEnd(value, at)
		}
		if depth == 0 {
			return at
		}
	}

	return len(value)
}

func pdfDictionaryLength(dictionary []byte) int {
	if !bytes.HasPrefix(dictionary, []byte("<<")) {
		return 0
	}
	end := pdfCompositeEnd(dictionary, 0)
	if end < 4 || !bytes.HasSuffix(dictionary[:end], []byte(">>")) {
		return 0
	}

	return end
}

func pdfDirectDictionary(value []byte) []byte {
	value = value[pdfSkipSpace(value, 0):]
	length := pdfDictionaryLength(value)
	if length == 0 {
		return nil
	}

	return value[:length]
}

func pdfReferenceAt(value []byte, at int) (string, int) {
	start := at
	_, firstEnd, first := pdfObjectStreamInteger(value, at)
	if !first || firstEnd >= len(value) || !pdfWhiteSpace(value[firstEnd]) {
		return "", start
	}
	secondStart := pdfSkipSpace(value, firstEnd)
	_, secondEnd, second := pdfObjectStreamInteger(value, secondStart)
	if !second || secondEnd >= len(value) || !pdfWhiteSpace(value[secondEnd]) {
		return "", start
	}
	end := pdfSkipSpace(value, secondEnd)
	if !pdfKeywordAt(value, end, "R") {
		return "", start
	}

	return string(value[at:firstEnd]) + " " + string(value[secondStart:secondEnd]), end + 1
}

func pdfKeywordAt(value []byte, at int, keyword string) bool {
	end := at + len(keyword)

	return end <= len(value) && string(value[at:end]) == keyword &&
		(end == len(value) || pdfWhiteSpace(value[end]) || pdfNameDelimiter(value[end]))
}

func pdfLeadingReference(value []byte) string {
	reference, _ := pdfReferenceAt(value, pdfSkipSpace(value, 0))

	return reference
}

func pdfLineEnd(value []byte, at int) int {
	for at < len(value) && value[at] != '\r' && value[at] != '\n' {
		at++
	}

	return at
}

func pdfHexEnd(value []byte, at int) int {
	if end := bytes.IndexByte(value[at:], '>'); end >= 0 {
		return at + end + 1
	}

	return len(value)
}
