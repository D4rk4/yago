package yagopdf

import "math"

type pdfGlyphWidthBudget struct {
	remainingEntries int
}

type pdfGlyphWidthArray struct {
	scanner pdfContentScanner
	budget  *pdfGlyphWidthBudget
}

func (b *pdfGlyphWidthBudget) take() bool {
	if b.remainingEntries == 0 {
		return false
	}
	b.remainingEntries--

	return true
}

func (a *pdfGlyphWidthArray) numbers(limit int) ([]float64, bool) {
	widths := make([]float64, 0, min(limit, 64))
	for {
		token, ok := a.scanner.next(0)
		if ok && token.kind == pdfContentArrayEnd {
			return widths, true
		}
		if !ok || token.kind != pdfContentNumber || len(widths) >= limit || !a.budget.take() {
			return nil, false
		}
		widths = append(widths, token.number)
	}
}

func (a *pdfGlyphWidthArray) cidRanges() ([]pdfGlyphWidthRange, bool) {
	ranges := make([]pdfGlyphWidthRange, 0, 16)
	for {
		span, more, valid := a.cidRange()
		if !valid {
			return nil, false
		}
		if !more {
			return pdfDisjointCodeSpans(ranges), true
		}
		ranges = append(ranges, span)
	}
}

func (a *pdfGlyphWidthArray) cidRange() (pdfGlyphWidthRange, bool, bool) {
	token, ok := a.scanner.next(0)
	if ok && token.kind == pdfContentArrayEnd {
		return pdfGlyphWidthRange{}, false, true
	}
	low, valid := pdfCharacterIdentifier(token, ok)
	if !valid {
		return pdfGlyphWidthRange{}, false, false
	}
	token, ok = a.scanner.next(0)
	if ok && token.kind == pdfContentArrayStart {
		widths, valid := a.numbers(pdfMaxCharacterIdentifier + 1 - int(low))
		return pdfGlyphWidthSpan(low, widths), true, valid && len(widths) > 0
	}
	high, valid := pdfCharacterIdentifier(token, ok)
	width, ok := a.scanner.next(0)
	if !valid || high < low || !ok || width.kind != pdfContentNumber || !a.budget.take() {
		return pdfGlyphWidthRange{}, false, false
	}

	return pdfGlyphWidthRange{low: low, high: high, width: width.number}, true, true
}

func pdfCharacterIdentifier(token pdfContentToken, ok bool) (uint32, bool) {
	if !ok || token.kind != pdfContentNumber {
		return 0, false
	}

	return pdfWholeCode(token.number, pdfMaxCharacterIdentifier)
}

func pdfWholeCode(value float64, ceiling uint32) (uint32, bool) {
	if value < 0 || value > float64(ceiling) || value != math.Trunc(value) {
		return 0, false
	}

	return uint32(value), true
}

func pdfLeadingNumber(value []byte) (float64, bool) {
	scanner := pdfContentScanner{content: value}
	token, ok := scanner.next(0)

	return token.number, ok && token.kind == pdfContentNumber
}

func pdfGlyphWidthSpan(first uint32, widths []float64) pdfGlyphWidthRange {
	last := first
	for index := 1; index < len(widths); index++ {
		last++
	}

	return pdfGlyphWidthRange{low: first, high: last, widths: widths}
}
