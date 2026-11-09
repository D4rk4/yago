package yagopdf

func (r *pdfFontResources) simpleWidthArray(value []byte) []float64 {
	key := pdfDictionaryIdentityOf(value)
	if cached, parsed := r.widthArrays[key]; parsed {
		return cached
	}
	array, opened := r.widthArray(value)
	var widths []float64
	if opened {
		widths, _ = array.numbers(pdfMaxSimpleFontCode + 1)
	}
	r.widthArrays[key] = widths

	return widths
}

func (r *pdfFontResources) cidWidthArray(value []byte) []pdfGlyphWidthRange {
	key := pdfDictionaryIdentityOf(value)
	if cached, parsed := r.cidWidthArrays[key]; parsed {
		return cached
	}
	array, opened := r.widthArray(value)
	var ranges []pdfGlyphWidthRange
	if opened {
		ranges, _ = array.cidRanges()
	}
	r.cidWidthArrays[key] = ranges

	return ranges
}

func (r *pdfFontResources) widthArray(value []byte) (*pdfGlyphWidthArray, bool) {
	array := &pdfGlyphWidthArray{
		scanner: pdfContentScanner{content: value},
		budget:  r.widthBudget,
	}
	token, ok := array.scanner.next(0)

	return array, ok && token.kind == pdfContentArrayStart
}
