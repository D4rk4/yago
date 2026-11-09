package yagopdf

import (
	"strings"
	"testing"
)

func TestPDFTextPositionSeparatesWordsAndLines(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    string
	}{
		{"adjacent glyphs", "BT /F1 10 Tf (A) Tj 5 0 Td (B) Tj ET", "AB"},
		{"word gap", "BT /F1 10 Tf (A) Tj 8 0 Td (B) Tj ET", "A B"},
		{"tracking", "BT /F1 10 Tf (A) Tj 5.1 0 Td (B) Tj ET", "AB"},
		{"backwards", "BT /F1 10 Tf (A) Tj -5 0 Td (B) Tj ET", "A\nB"},
		{"new line", "BT /F1 10 Tf (A) Tj 0 -12 Td (B) Tj ET", "A\nB"},
		{"text scale", "BT /F1 1 Tf 12 0 0 -12 0 0 Tm (A) Tj 0 -1.5 Td (B) Tj ET", "A\nB"},
		{"rotated", "0 1 -1 0 0 0 cm BT /F1 10 Tf (A) Tj 8 0 Td (B) Tj ET", "A B"},
		{"horizontal scale", "BT /F1 10 Tf 200 Tz (A) Tj 10 0 Td (B) Tj ET", "AB"},
		{"character spacing", "BT /F1 10 Tf 2 Tc (A) Tj 7 0 Td (B) Tj ET", "AB"},
		{"word spacing", "BT /F1 10 Tf 2 Tw (A ) Tj 12 0 Td (B) Tj ET", "A B"},
		{"leading", "BT /F1 10 Tf 12 TL (A) Tj T* (B) Tj ET", "A\nB"},
		{"set leading", "BT /F1 10 Tf (A) Tj 0 -12 TD (B) Tj T* (C) Tj ET", "A\nB\nC"},
		{"rise", "BT /F1 10 Tf (A) Tj 1 Ts (B) Tj ET", "AB"},
		{"font outside block", "/F1 10 Tf BT (A) Tj (B) Tj ET", "AB"},
		{"saved state", "BT /F1 10 Tf q 2 Tc (A) Tj Q 7 0 Td (B) Tj ET", "AB"},
		{"singular transform", "0 0 0 0 0 0 cm BT /F1 10 Tf (A) Tj (B) Tj ET", "AB"},
		{"small array kern", "BT /F1 10 Tf [(A) -179 (B)] TJ ET", "AB"},
		{"large array kern", "BT /F1 10 Tf [(A) -180 (B)] TJ ET", "A B"},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := newPDFTextCollector(pdfMaxTextBytes)
			pdfWriteContentText(out, []byte(test.content), spacingFontTables())
			got := strings.TrimSpace(pdfCollapseHorizontalWhitespace(out.String()))
			if got != test.want {
				t.Fatalf("positioned text = %q, want %q", got, test.want)
			}
		})
	}
}

func spacingFontTables() map[string]*pdfCMap {
	body := []byte("/F1 1 0 R\n" +
		"1 0 obj << /Type /Font /Encoding /WinAnsiEncoding /FirstChar 32 /Widths [" +
		strings.Repeat("500 ", 95) + "] >> endobj\n")
	return pdfTextTablesWithQuota(body, newPDFDecodeQuota(pdfMaxDecodedDocumentBytes))
}

func TestPDFGraphicsStackIsBounded(t *testing.T) {
	out := newPDFTextCollector(pdfMaxTextBytes)
	interpreter := newPDFTextInterpreter(out, nil)
	interpreter.read([]byte(strings.Repeat("q ", pdfMaxGraphicsDepth+2)))
	if len(interpreter.saved) != pdfMaxGraphicsDepth || interpreter.overflow != 2 {
		t.Fatalf("graphics stack = %d, overflow = %d", len(interpreter.saved), interpreter.overflow)
	}
	interpreter.read([]byte(strings.Repeat("Q ", pdfMaxGraphicsDepth+3)))
	if len(interpreter.saved) != 0 || interpreter.overflow != 0 {
		t.Fatalf(
			"restored graphics stack = %d, overflow = %d",
			len(interpreter.saved),
			interpreter.overflow,
		)
	}
	interpreter.read([]byte("BT (visible letters) Tj ET"))
	if strings.TrimSpace(out.String()) != "visible letters" {
		t.Fatalf("text after stack recovery = %q", out.String())
	}
}
