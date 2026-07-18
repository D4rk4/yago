package formatparse

import "strings"

const (
	pdfMaxPDFNameBytes   = 127
	pdfMaxGlyphNameBytes = 63
)

func pdfDecodedNameToken(value []byte, limit int) (string, int, bool) {
	if len(value) == 0 || value[0] != '/' || limit <= 0 {
		return "", 0, false
	}
	end := 1
	for end < len(value) && !pdfWhiteSpace(value[end]) && !pdfNameDelimiter(value[end]) {
		end++
	}
	var decoded strings.Builder
	for at := 1; at < end; at++ {
		current := value[at]
		if current == '#' && at+2 < end {
			high, highOK := pdfHexNibble(value[at+1])
			low, lowOK := pdfHexNibble(value[at+2])
			if highOK && lowOK {
				current = high<<4 | low
				at += 2
			}
		}
		if decoded.Len() >= limit {
			return "", end, false
		}
		_ = decoded.WriteByte(current)
	}
	if decoded.Len() == 0 {
		return "", end, false
	}

	return decoded.String(), end, true
}

func pdfNameDelimiter(value byte) bool {
	return value == '(' || value == ')' || value == '<' || value == '>' ||
		value == '[' || value == ']' || value == '{' || value == '}' ||
		value == '/' || value == '%'
}

func pdfWhiteSpace(value byte) bool {
	return value == 0 || value == '\t' || value == '\n' || value == '\f' ||
		value == '\r' || value == ' '
}

func pdfHexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	default:
		return 0, false
	}
}
