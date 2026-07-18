package formatparse

import (
	"encoding/hex"
	"unicode/utf16"
	"unicode/utf8"
)

func pdfCMapUnicodeText(digits []byte) (string, bool) {
	if len(digits) == 0 {
		return "", false
	}
	compact := digits
	if len(compact)%2 == 1 {
		compact = append(append([]byte{}, compact...), '0')
	}
	raw := make([]byte, hex.DecodedLen(len(compact)))
	if _, err := hex.Decode(raw, compact); err != nil || len(raw)%2 != 0 {
		return "", false
	}
	runes := make([]rune, 0, len(raw)/2)
	for at := 0; at < len(raw); at += 2 {
		first := rune(uint16(raw[at])<<8 | uint16(raw[at+1]))
		switch {
		case first >= 0xD800 && first <= 0xDBFF:
			if at+3 >= len(raw) {
				return "", false
			}
			second := rune(uint16(raw[at+2])<<8 | uint16(raw[at+3]))
			if second < 0xDC00 || second > 0xDFFF {
				return "", false
			}
			runes = append(runes, utf16.DecodeRune(first, second))
			at += 2
		case first >= 0xDC00 && first <= 0xDFFF:
			return "", false
		default:
			runes = append(runes, first)
		}
	}

	return string(runes), len(runes) > 0
}

func pdfIncrementedCMapText(text string, delta uint32) (string, bool) {
	runes := []rune(text)
	if len(runes) == 0 {
		return "", false
	}
	last := uint64(runes[len(runes)-1]) + uint64(delta)
	if last > utf8.MaxRune || !pdfUnicodeScalar(rune(last)) {
		return "", false
	}
	runes[len(runes)-1] = rune(last)

	return string(runes), true
}
