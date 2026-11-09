package yagopdf

import (
	"cmp"
	"slices"
)

type pdfCMapRange struct {
	low  uint32
	high uint32
	text string
}

type pdfCodeSpan interface {
	bounds() (uint32, uint32)
}

func (cmap *pdfCMap) codeText(code uint32) (string, bool) {
	if text, ok := cmap.text[code]; ok {
		return text, true
	}
	at, found := slices.BinarySearchFunc(cmap.ranges, code, pdfCodeSpanPosition)
	if !found {
		return "", false
	}

	return pdfIncrementedCMapText(cmap.ranges[at].text, code-cmap.ranges[at].low)
}

func (span pdfCMapRange) bounds() (uint32, uint32) {
	return span.low, span.high
}

func pdfCodeSpanPosition[Span pdfCodeSpan](candidate Span, code uint32) int {
	low, high := candidate.bounds()
	switch {
	case high < code:
		return -1
	case low > code:
		return 1
	default:
		return 0
	}
}

func pdfDisjointCodeSpans[Span pdfCodeSpan](spans []Span) []Span {
	slices.SortStableFunc(spans, func(left, right Span) int {
		leftLow, _ := left.bounds()
		rightLow, _ := right.bounds()

		return cmp.Compare(leftLow, rightLow)
	})
	disjoint := spans[:0]
	for _, candidate := range spans {
		low, _ := candidate.bounds()
		if len(disjoint) > 0 {
			if _, previousHigh := disjoint[len(disjoint)-1].bounds(); low <= previousHigh {
				continue
			}
		}
		disjoint = append(disjoint, candidate)
	}

	return disjoint
}
