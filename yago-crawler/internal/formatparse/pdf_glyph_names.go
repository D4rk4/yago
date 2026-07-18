package formatparse

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

func pdfGlyphNameText(name string) string {
	if suffix := strings.IndexByte(name, '.'); suffix >= 0 {
		name = name[:suffix]
	}
	var text strings.Builder
	for component := range strings.SplitSeq(name, "_") {
		text.WriteString(pdfGlyphComponentText(component))
	}

	return text.String()
}

func pdfGlyphComponentText(component string) string {
	if text, exists := pdfStandardGlyphText[component]; exists {
		return text
	}
	if len(component) == 1 && printableASCII(component[0]) {
		return component
	}
	if strings.HasPrefix(component, "uni") {
		return pdfGlyphUnicodeSequence(component[3:])
	}
	if strings.HasPrefix(component, "u") {
		return pdfGlyphUnicodeScalar(component[1:])
	}

	return ""
}

func pdfGlyphUnicodeSequence(digits string) string {
	if len(digits) == 0 || len(digits)%4 != 0 || !pdfUpperHex(digits) {
		return ""
	}
	runes := make([]rune, 0, len(digits)/4)
	for at := 0; at < len(digits); at += 4 {
		value, _ := strconv.ParseUint(digits[at:at+4], 16, 16)
		if !pdfUnicodeScalar(rune(value)) {
			return ""
		}
		runes = append(runes, rune(value))
	}

	return string(runes)
}

func pdfGlyphUnicodeScalar(digits string) string {
	if len(digits) < 4 || len(digits) > 6 || !pdfUpperHex(digits) {
		return ""
	}
	value, _ := strconv.ParseUint(digits, 16, 32)
	if value > uint64(utf8.MaxRune) {
		return ""
	}
	if !pdfUnicodeScalar(rune(value)) {
		return ""
	}

	return string(rune(value))
}

func pdfUpperHex(value string) bool {
	for index := range len(value) {
		character := value[index]
		if character >= '0' && character <= '9' || character >= 'A' && character <= 'F' {
			continue
		}

		return false
	}

	return true
}

func pdfUnicodeScalar(value rune) bool {
	return utf8.ValidRune(value) && (value < 0xD800 || value > 0xDFFF)
}
