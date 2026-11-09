package yagopdf

import (
	"math"
	"strings"
	"testing"
)

func TestPDFTextOperatorsRequireShownOperands(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    string
	}{
		{"literal operators", "BT (MARKET REPORT) Tj ( SUBTOTAL) Tj ET", "MARKET REPORT SUBTOTAL"},
		{"no implicit show", "BT (hidden operand) pop (visible letters) Tj ET", "visible letters"},
		{"outside block", "(hidden outside) Tj BT (visible letters) Tj ET", "visible letters"},
		{"comments", "BT % ET (hidden comment) Tj\n(visible letters) Tj ET", "visible letters"},
		{"marked content", "BT /Span << /ActualText (hidden) >> BDC (visible letters) Tj EMC ET", "visible letters"},
		{"inline image", "BI /W 1 /H 1 ID BT (hidden image) Tj ET EI BT (visible letters) Tj ET", "visible letters"},
		{"quotes", "BT (first line) Tj (second line) ' 1 2 (third line) \" ET", "first line\nsecond line\nthird line"},
		{"invalid quote", "BT (hidden) 2 \" (visible letters) Tj ET", "visible letters"},
		{"missing show operand", "BT Tj 12 Tj (visible letters) Tj ET", "visible letters"},
		{"invalid font", "BT /F1 (not a size) Tf (visible letters) Tj ET", "visible letters"},
		{"missing font operand", "BT 12 Tf (visible letters) Tj ET", "visible letters"},
		{"invalid quote spacing", "BT (not a number) 2 (hidden letters) \" (visible letters) Tj ET", "visible letters"},
		{"invalid matrix", "BT 1 0 0 (bad) 0 0 Tm (visible letters) Tj ET", "visible letters"},
		{"array kern", "BT [(word) -31 (part) -180 (next)] TJ ET", "wordpart next"},
		{"invalid nested array", "BT [[(hidden)] (also hidden)] TJ (visible letters) Tj ET", "visible letters"},
		{"unterminated array", "BT [(hidden) TJ", ""},
		{"unshown array", "BT [(hidden)] pop (visible letters) Tj ET", "visible letters"},
		{"overflow operands", "BT 1 2 3 4 5 6 (hidden) Tj (visible letters) Tj ET", "visible letters"},
		{"unknown font", "BT /Missing 12 Tf (hidden letters) Tj ET", ""},
		{"empty show", "BT () Tj (visible letters) Tj ET", "visible letters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := newPDFTextCollector(pdfMaxTextBytes)
			pdfWriteContentText(out, []byte(test.content), nil)
			got := strings.TrimSpace(pdfCollapseHorizontalWhitespace(out.String()))
			if got != test.want {
				t.Fatalf("text = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPDFMatrixCompositionAndInverse(t *testing.T) {
	matrix := pdfMatrix{2, 0, 0, -3, 4, 5}
	translated := matrix.translated(7, 11)
	if translated != (pdfMatrix{2, 0, 0, -3, 18, -28}) {
		t.Fatalf("translated matrix = %v", translated)
	}
	if pdfIdentityMatrix.multiply(matrix) != matrix ||
		matrix.multiply(pdfIdentityMatrix) != matrix {
		t.Fatal("identity changed matrix")
	}
	horizontal, vertical, ok := matrix.linearInverse(14, -33)
	if !ok || horizontal != 7 || vertical != 11 {
		t.Fatalf("inverse = %g %g %t", horizontal, vertical, ok)
	}
	if _, _, ok := (pdfMatrix{}).linearInverse(1, 2); ok {
		t.Fatal("singular matrix admitted")
	}
	rotation := pdfMatrix{0, 1, -1, 0, 0, 0}
	if horizontal, vertical, ok = rotation.linearInverse(
		-3,
		7,
	); !ok || horizontal != 7 ||
		vertical != 3 {
		t.Fatalf("rotation inverse = %g %g %t", horizontal, vertical, ok)
	}
}

func TestPDFTextNumbersRejectMalformedOperands(t *testing.T) {
	for _, operands := range [][]pdfContentToken{
		{},
		{{kind: pdfContentString}},
		{{kind: pdfContentNumber, number: math.Inf(1)}},
		{{kind: pdfContentNumber, number: math.NaN()}},
	} {
		if _, ok := pdfTextNumbers(operands, 1); ok {
			t.Fatalf("invalid operands admitted: %v", operands)
		}
	}
	if numbers, ok := pdfTextNumbers(
		[]pdfContentToken{{kind: pdfContentNumber, number: 12}},
		1,
	); !ok ||
		numbers[0] != 12 {
		t.Fatalf("valid operands = %v %t", numbers, ok)
	}
}
