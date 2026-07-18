package formatparse

func pdfTextFontSelection(
	value []byte,
	tables map[string]*pdfCMap,
	current *pdfCMap,
) (*pdfCMap, int, bool) {
	name, consumed, selected := pdfFontSelection(value)
	if !selected {
		return current, consumed, false
	}
	if table, exists := tables[name]; exists {
		return table, consumed, true
	}

	return pdfUnavailableFontTable(), consumed, true
}

func pdfFontSelection(value []byte) (string, int, bool) {
	name, consumed, valid := pdfDecodedNameToken(value, pdfMaxPDFNameBytes)
	if !valid {
		return "", consumed, false
	}
	at := pdfSkipFontSelectionSpace(value, consumed)
	numberEnd, valid := pdfFontSizeEnd(value, at)
	if !valid {
		return "", consumed, false
	}
	operator := pdfSkipFontSelectionSpace(value, numberEnd)
	if operator == numberEnd || operator+2 > len(value) ||
		value[operator] != 'T' || value[operator+1] != 'f' ||
		operator+2 < len(value) && !pdfWhiteSpace(value[operator+2]) &&
			!pdfNameDelimiter(value[operator+2]) {
		return "", consumed, false
	}

	return name, operator + 2, true
}

func pdfSkipFontSelectionSpace(value []byte, at int) int {
	for at < len(value) && pdfWhiteSpace(value[at]) {
		at++
	}

	return at
}

func pdfFontSizeEnd(value []byte, at int) (int, bool) {
	if at < len(value) && (value[at] == '+' || value[at] == '-') {
		at++
	}
	digits := 0
	dots := 0
	for at < len(value) {
		switch {
		case value[at] >= '0' && value[at] <= '9':
			digits++
			at++
		case value[at] == '.' && dots == 0:
			dots++
			at++
		default:
			return at, digits > 0
		}
	}

	return at, digits > 0
}
