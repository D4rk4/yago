package formatparse

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPDFPageTextSharesDecodedQuotaAcrossStreams(t *testing.T) {
	first := "BT (first bounded searchable section) Tj ET"
	second := "BT (second section must not be decoded) Tj ET"
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.7\n")
	writePDFStreamObject(t, &pdf, 1, "", first)
	writePDFStreamObject(t, &pdf, 2, "", second)
	pdf.WriteString("%%EOF")
	quota := newPDFDecodeQuota(len(first))
	text := pdfPageText(pdf.Bytes(), nil, quota, pdfMaxTextBytes)
	if !strings.Contains(text, "first bounded searchable section") {
		t.Fatalf("first stream text = %q", text)
	}
	if strings.Contains(text, "second section") {
		t.Fatalf("decoded text exceeded aggregate quota: %q", text)
	}
	if !quota.exhausted() {
		t.Fatalf("remaining decode quota = %d", quota.remainingBytes)
	}
	streamQuota := newPDFDecodeQuota(len(first))
	streams := pdfContentStreamsWithQuota(pdf.Bytes(), streamQuota)
	if len(streams) != 1 || string(streams[0]) != first || !streamQuota.exhausted() {
		t.Fatalf("bounded streams = %d/%d", len(streams), streamQuota.remainingBytes)
	}
	emptyQuota := newPDFDecodeQuota(0)
	if streams := pdfContentStreamsWithQuota(pdf.Bytes(), emptyQuota); len(streams) != 0 {
		t.Fatalf("zero-quota streams = %d", len(streams))
	}
	decoded, decodedWorkBytes, ok := pdfDecodeChainWithin([]byte("raw"), nil, 2)
	if !ok || string(decoded) != "ra" || decodedWorkBytes != 2 {
		t.Fatalf("unfiltered decode = %q/%d/%t", decoded, decodedWorkBytes, ok)
	}
}

func TestPDFObjectStreamsShareDecodedQuota(t *testing.T) {
	content := "begincmap bounded decoded object endcmap"
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.7\n")
	writePDFStreamObject(t, &pdf, 1, "", content)
	writePDFStreamObject(t, &pdf, 2, "", content)
	pdf.WriteString("%%EOF")
	quota := newPDFDecodeQuota(len(content))
	lookup := newPDFObjectLookup(pdf.Bytes())
	if decoded := pdfObjectStreamWithQuota(lookup.value, "1", quota); string(decoded) != content {
		t.Fatalf("first object stream = %q", decoded)
	}
	if decoded := pdfObjectStreamWithQuota(lookup.value, "2", quota); decoded != nil {
		t.Fatalf("second object stream = %q", decoded)
	}
	if !quota.exhausted() {
		t.Fatalf("remaining decode quota = %d", quota.remainingBytes)
	}
}

func TestPDFShownStringStopsAtValidUTF8Prefix(t *testing.T) {
	const limit = 10
	literal := "(" + strings.Repeat("A", 1<<20) + ") Tj"
	raw, consumed := pdfRawStringLiteralWithin([]byte(literal), limit*2)
	if len(raw) != limit*2 || consumed != len(literal)-len(" Tj")-1 {
		t.Fatalf("bounded raw literal = %d/%d", len(raw), consumed)
	}
	text := newPDFTextCollector(limit)
	pdfWriteShownStrings(
		text,
		[]byte(literal),
		nil,
		&pdfCMap{codeLen: 1, text: map[uint32]string{'A': "€"}},
	)
	if text.String() != "€€€" || len(text.String()) > limit || !utf8.ValidString(text.String()) {
		t.Fatalf("bounded mapped text = %q (%d bytes)", text.String(), len(text.String()))
	}
}

func TestPDFTextCollectorHandlesOperandEdges(t *testing.T) {
	complete := newPDFTextCollector(0)
	if complete.rawOperandLimit() != 0 {
		t.Fatalf("complete operand limit = %d", complete.rawOperandLimit())
	}
	complete.writeByte('A')
	complete.writeRune('A')
	complete.writeString("A")
	if complete.String() != "" {
		t.Fatalf("complete collector = %q", complete.String())
	}
	partialRune := newPDFTextCollector(1)
	partialRune.writeRune('€')
	if !partialRune.complete() || partialRune.String() != "" {
		t.Fatalf("partial rune = %q", partialRune.String())
	}
	invalidRune := newPDFTextCollector(4)
	invalidRune.writeRune(0xD800)
	if invalidRune.String() != "�" {
		t.Fatalf("invalid rune = %q", invalidRune.String())
	}
	raw, consumed := pdfRawStringLiteralWithin([]byte("(outer (inner)\n\\101)"), 64)
	if string(raw) != "outer (inner) A" || consumed != len("(outer (inner)\n\\101)")-1 {
		t.Fatalf("nested bounded literal = %q/%d", raw, consumed)
	}
	raw, consumed = pdfRawStringLiteralWithin([]byte("(unterminated"), 64)
	if string(raw) != "unterminated" || consumed != len("(unterminated") {
		t.Fatalf("unterminated bounded literal = %q/%d", raw, consumed)
	}
	if raw, consumed = pdfRawHexStringWithin([]byte("<4142"), 2); raw != nil ||
		consumed != len("<4142") {
		t.Fatalf("unterminated bounded hex = %q/%d", raw, consumed)
	}
	if raw, consumed = pdfRawHexStringWithin([]byte("<zz>"), 2); raw != nil || consumed != 3 {
		t.Fatalf("invalid bounded hex = %q/%d", raw, consumed)
	}
	if raw, consumed = pdfRawHexStringWithin([]byte("<4142>"), 2); string(raw) != "AB" ||
		consumed != 5 {
		t.Fatalf("bounded hex = %q/%d", raw, consumed)
	}
	if raw, consumed = pdfRawHexString([]byte("<4142>")); string(raw) != "AB" || consumed != 5 {
		t.Fatalf("raw hex = %q/%d", raw, consumed)
	}
}

func TestPDFBoundedTextDecodesMappedAndUTF16Values(t *testing.T) {
	mapped := newPDFTextCollector(16)
	pdfWriteShownText(
		mapped,
		[]byte{0x00, 0x41, 'Z'},
		&pdfCMap{codeLen: 2, text: map[uint32]string{0x0041: "Ы"}},
	)
	if mapped.String() != "ЫZ" {
		t.Fatalf("bounded mapped text = %q", mapped.String())
	}
	decoded := newPDFTextCollector(32)
	pdfWriteDecodedStringBytes(decoded, []byte{
		0xFE, 0xFF,
		0x00, 'H',
		0x00, 0x09,
		0xD8, 0x3D, 0xDE, 0x00,
		0xD8, 0x00,
	})
	if decoded.String() != "H 😀�" || !utf8.ValidString(decoded.String()) {
		t.Fatalf("bounded UTF-16 text = %q", decoded.String())
	}
	ligature := newPDFTextCollector(3)
	pdfWriteDecodedStringBytes(ligature, []byte{0x1C})
	if ligature.String() != "fi" {
		t.Fatalf("bounded ligature = %q", ligature.String())
	}
}

func TestPDFCMapQuotaBoundsRangeExpansion(t *testing.T) {
	quota := newPDFCMapQuota(10, 4)
	table := pdfParseCMapWithQuota([]byte(
		"beginbfrange\n<41> <45> <00410042>\nendbfrange\n",
	), quota)
	if table == nil || len(table.text) != 2 || table.text[0x41] != "AB" ||
		table.text[0x42] != "AC" {
		t.Fatalf("bounded CMap = %#v", table)
	}
	if quota.remainingEntries != 0 || quota.remainingTextBytes != 0 {
		t.Fatalf(
			"remaining CMap quota = %d entries/%d bytes",
			quota.remainingEntries,
			quota.remainingTextBytes,
		)
	}
	if table := pdfParseCMap([]byte(
		"beginbfrange\n<41> <41> <0>\nendbfrange\n",
	)); table != nil {
		t.Fatalf("empty range destination = %#v", table)
	}
	if table := pdfParseCMapWithQuota([]byte(
		"beginbfchar\n<41> <0041>\nendbfchar\n",
	), newPDFCMapQuota(1, 0)); table != nil {
		t.Fatalf("zero-quota CMap = %#v", table)
	}
}

func TestPDFTextLimitHoldsThroughParse(t *testing.T) {
	content := "BT (" + strings.Repeat("Aé", pdfMaxTextBytes/2) + ") Tj ET"
	page, parsed := parsePDF(
		"https://example.test/bounded.pdf",
		"application/pdf",
		pdfWithText(t, "", content),
	)
	if !parsed {
		t.Fatal("bounded PDF did not parse")
	}
	if len(page.Text) > pdfMaxTextBytes || !utf8.ValidString(page.Text) {
		t.Fatalf(
			"parsed text = %d bytes, valid UTF-8 = %t",
			len(page.Text),
			utf8.ValidString(page.Text),
		)
	}
}

func TestPDFTitleLimitKeepsValidUTF8(t *testing.T) {
	body := []byte("%PDF-1.7\n<< /Title (" +
		strings.Repeat("Aé", pdfMaxTextBytes/2) + ") >>")
	title := pdfInfoTitle(body)
	if len(title) > pdfMaxTextBytes || !utf8.ValidString(title) {
		t.Fatalf("title = %d bytes, valid UTF-8 = %t", len(title), utf8.ValidString(title))
	}
}
