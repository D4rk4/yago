package yagoproto

import "unicode"

const (
	maximumJavaSignedInt32 = int64(1<<31 - 1)
	minimumJavaSignedInt32 = int64(-1 << 31)
)

func parseJavaSignedDecimalInt32(value string) (int, bool) {
	if value == "" {
		return 0, false
	}

	negative := false
	digits := value
	switch value[0] {
	case '-':
		negative = true
		digits = value[1:]
	case '+':
		digits = value[1:]
	}
	if digits == "" {
		return 0, false
	}

	limit := maximumJavaSignedInt32
	if negative {
		limit = -minimumJavaSignedInt32
	}
	parsed := int64(0)
	for _, character := range digits {
		digit, valid := javaDecimalDigit(character)
		if !valid || parsed > (limit-int64(digit))/10 {
			return 0, false
		}
		parsed = parsed*10 + int64(digit)
	}
	if negative {
		parsed = -parsed
	}

	return int(parsed), true
}

func javaDecimalDigit(character rune) (int, bool) {
	if character > 0xffff {
		return 0, false
	}
	for _, digitRange := range unicode.Nd.R16 {
		if character < rune(digitRange.Lo) {
			return 0, false
		}
		if character > rune(digitRange.Hi) {
			continue
		}
		offset := int(character) - int(digitRange.Lo)

		return offset / int(digitRange.Stride) % 10, true
	}

	return 0, false
}
