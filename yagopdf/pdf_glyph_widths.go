package yagopdf

import "slices"

type pdfGlyphWidthRange struct {
	low    uint32
	high   uint32
	width  float64
	widths []float64
}

type pdfGlyphWidths struct {
	ranges       []pdfGlyphWidthRange
	missing      float64
	missingKnown bool
}

func (w *pdfGlyphWidths) width(code uint32) (float64, bool) {
	if w == nil {
		return 0, false
	}
	at, found := slices.BinarySearchFunc(w.ranges, code, pdfCodeSpanPosition)
	if !found {
		return w.missing, w.missingKnown
	}
	span := w.ranges[at]
	if span.widths == nil {
		return span.width, true
	}

	return span.widths[code-span.low], true
}

func (span pdfGlyphWidthRange) bounds() (uint32, uint32) {
	return span.low, span.high
}
