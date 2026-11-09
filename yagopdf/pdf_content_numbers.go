package yagopdf

func pdfContentNumberValue(raw []byte) (float64, bool) {
	sign := 1.0
	if len(raw) > 0 && (raw[0] == '+' || raw[0] == '-') {
		if raw[0] == '-' {
			sign = -1
		}
		raw = raw[1:]
	}
	value, scale := 0.0, 1.0
	fraction, digit := false, false
	for _, character := range raw {
		if character == '.' && !fraction {
			fraction = true
			continue
		}
		if character < '0' || character > '9' {
			return 0, false
		}
		digit = true
		if fraction {
			scale /= 10
			value += float64(character-'0') * scale
		} else {
			value = min(1e9, value*10+float64(character-'0'))
		}
	}
	return sign * value, digit
}
