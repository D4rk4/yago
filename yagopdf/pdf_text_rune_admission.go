package yagopdf

import "unicode"

const pdfFirstLatinLigature = 0xFB00

var pdfLatinLigatureLetters = [...]string{"ff", "fi", "fl", "ffi", "ffl", "st", "st"}

func pdfLatinLigatureText(value rune) (string, bool) {
	position := int(value) - pdfFirstLatinLigature
	if position < 0 || position >= len(pdfLatinLigatureLetters) {
		return "", false
	}

	return pdfLatinLigatureLetters[position], true
}

func pdfAdmittedTextRune(value rune) (rune, bool) {
	switch {
	case value == '\n' || value == '\t':
		return value, true
	case unicode.IsControl(value) && unicode.IsSpace(value):
		return ' ', true
	case unicode.IsControl(value) ||
		unicode.Is(unicode.Bidi_Control, value) ||
		unicode.Is(unicode.Noncharacter_Code_Point, value):
		return 0, false
	default:
		return value, true
	}
}
