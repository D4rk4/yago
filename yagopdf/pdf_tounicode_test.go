package yagopdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
)

func zlibBytes(t *testing.T, content string) []byte {
	t.Helper()
	var z bytes.Buffer
	writer := zlib.NewWriter(&z)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	_ = writer.Close()

	return z.Bytes()
}

// subsetFontPDF assembles a PDF whose subset font shows "fi" at code 0x02 —
// the modern-pdfTeX shape where only the embedded ToUnicode CMap can name the
// glyph (the live arXiv 1706.03762 regression).
func subsetFontPDF(t *testing.T, cmap string) []byte {
	t.Helper()
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	pdf.WriteString("4 0 obj\n<< /Type /Page /Resources << /Font << /F1 5 0 R >> >> " +
		"/Contents 7 0 R >>\nendobj\n")
	pdf.WriteString("5 0 obj\n<< /Type /Font /BaseFont /ABCDEF+X " +
		"/Encoding << /BaseEncoding /WinAnsiEncoding /Differences [2 /X] >> " +
		"/ToUnicode 6 0 R >>\nendobj\n")
	compressed := zlibBytes(t, cmap)
	fmt.Fprintf(&pdf, "6 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", len(compressed))
	pdf.Write(compressed)
	pdf.WriteString("\nendstream\nendobj\n")
	content := zlibBytes(t, `BT /F1 10 Tf (\002rmly established) Tj ET`)
	fmt.Fprintf(&pdf, "7 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n", len(content))
	pdf.Write(content)
	pdf.WriteString("\nendstream\nendobj\n%%EOF")

	return pdf.Bytes()
}

const subsetCMap = `/CIDInit /ProcSet findresource begin
begincmap
1 begincodespacerange
<00> <FF>
endcodespacerange
2 beginbfchar
<02> <00660069>
<123456789> <0041>
endbfchar
1 beginbfrange
<41> <43> <0410>
endbfrange
endcmap
`

func TestParsePDFAppliesToUnicode(t *testing.T) {
	page, parsed := ReadPDF(subsetFontPDF(t, subsetCMap))
	if !parsed || !strings.Contains(page.Text, "firmly established") {
		t.Fatalf("subset font text = %v %q", parsed, page.Text)
	}
}

func TestPDFObjectResolutionEdges(t *testing.T) {
	body := []byte("106 0 obj\n<< /A /B >>\nendobj\n6 0 obj\n<< /C /D >>\nendobj\n")
	lookup := newPDFObjectLookup(body)
	if got := lookup.value("6 0"); !bytes.Contains(got, []byte("/C")) {
		t.Fatalf("object 6 = %q", got)
	}
	if got := newPDFObjectLookup([]byte("5 0 obj\n<< /Tail >>")).value("5 0"); got != nil {
		t.Fatalf("unterminated object = %q", got)
	}
	if got := lookup.value("7 0"); got != nil {
		t.Fatalf("missing object = %q", got)
	}
	if got := pdfObjectStreamWithQuota(
		lookup.value,
		"7 0",
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); got != nil {
		t.Fatalf("missing object stream = %q", got)
	}
	if got := pdfObjectStreamWithQuota(
		lookup.value,
		"6 0",
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); got != nil {
		t.Fatalf("streamless object = %q", got)
	}
	raw := []byte("8 0 obj\n<< >>\nstream\r\nplain\nendstream\nendobj")
	if got := pdfObjectStreamWithQuota(
		newPDFObjectLookup(raw).value,
		"8 0",
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); string(got) != "plain\n" {
		t.Fatalf("unfiltered stream = %q", got)
	}
	unsupported := []byte("8 0 obj\n<< /Filter /DCTDecode >>\nstream\r\nplain\nendstream\nendobj")
	if got := pdfObjectStreamWithQuota(
		newPDFObjectLookup(unsupported).value,
		"8 0",
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); got != nil {
		t.Fatalf("unsupported filter stream = %q", got)
	}
	unterminated := []byte("9 0 obj\n<< >>\nstream\nabc")
	if got := pdfObjectStreamWithQuota(
		newPDFObjectLookup(unterminated).value,
		"9 0",
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); got != nil {
		t.Fatalf("unterminated stream = %q", got)
	}
	if got := pdfObjectStreamWithQuota(
		func(string) []byte { return []byte("<< >>\nstream\nunterminated") },
		"9 0",
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); got != nil {
		t.Fatalf("unterminated direct stream = %q", got)
	}
}

// TestPDFCMapParsingEdges pins the CMap reader: nil and mapless sources yield
// no table, the array bfrange form and malformed rows skip, reversed ranges
// skip, the codespace width picks the code size, and the entry cap holds.
func TestPDFCMapParsingEdges(t *testing.T) {
	if pdfParseCMapWithQuota(nil, newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes)) != nil {
		t.Fatal("nil cmap must yield no table")
	}
	if pdfParseCMapWithQuota(
		[]byte("begincmap endcmap"),
		newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes),
	) != nil {
		t.Fatal("mapless cmap must yield no table")
	}
	src := []byte(`1 begincodespacerange
<0000> <FFFF>
endcodespacerange
3 beginbfrange
<0041> <0042> [<0057> <0058>]
<0044> <0043> <0057>
<0046> <0046> <zz>
endbfrange
1 beginbfchar
<0045> <0057>
endbfchar
`)
	table := pdfParseCMapWithQuota(src, newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes))
	if table == nil || table.codeLen != 2 {
		t.Fatalf("table = %+v", table)
	}
	for _, skipped := range []uint32{0x41, 0x44, 0x46} {
		if text, mapped := table.codeText(skipped); mapped {
			t.Fatalf("skipped row %#x leaked %q", skipped, text)
		}
	}
	if text, _ := table.codeText(0x45); text != "W" {
		t.Fatalf("bfchar = %q", text)
	}
}

func TestPDFCMapRanges(t *testing.T) {
	identityQuota := newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes)
	identity := pdfParseCMapWithQuota(
		[]byte(
			"1 beginbfrange\n<0000> <FFFF> <0000>\n<0041> <0042> <0058>\n<0100> <0100> <0059>\nendbfrange\n",
		),
		identityQuota,
	)
	for code, want := range map[uint32]string{0x0041: "A", 0x0436: "ж", 0xFFFD: "\uFFFD"} {
		if text, mapped := identity.codeText(code); !mapped || text != want {
			t.Fatalf("identity range %#x = %q %t, want %q", code, text, mapped, want)
		}
	}
	if text, mapped := identity.codeText(0xD800); mapped {
		t.Fatalf("surrogate code mapped to %q", text)
	}
	if len(identity.text) != 0 || len(identity.ranges) != 1 ||
		identityQuota.remainingEntries != pdfMaxCMapEntries-3 {
		t.Fatalf("identity range storage = %d entries, %d ranges, %d quota left",
			len(identity.text), len(identity.ranges), identityQuota.remainingEntries)
	}
	disjoint := pdfParseCMapWithQuota(
		[]byte(
			"1 beginbfrange\n<0100> <0100> <0059>\n<0041> <0050> <0061>\n<0045> <0046> <0058>\nendbfrange\n",
		),
		newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes),
	)
	for code, want := range map[uint32]string{0x45: "e", 0x50: "p", 0x100: "Y"} {
		if text, mapped := disjoint.codeText(code); !mapped || text != want {
			t.Fatalf("disjoint range %#x = %q %t, want %q", code, text, mapped, want)
		}
	}
	if _, mapped := disjoint.codeText(0x51); mapped {
		t.Fatal("gap between disjoint ranges must stay unmapped")
	}
}

func TestPDFCMapEntryCap(t *testing.T) {
	var charCapped strings.Builder
	charCapped.WriteString("beginbfchar\n")
	for i := 0; i < pdfMaxCMapEntries+8; i++ {
		fmt.Fprintf(&charCapped, "<%04X> <0041>\n", i)
	}
	charCapped.WriteString("endbfchar\n")
	charTable := pdfParseCMapWithQuota(
		[]byte(charCapped.String()),
		newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes),
	)
	if len(charTable.text) != pdfMaxCMapEntries {
		t.Fatalf("char cap = %d", len(charTable.text))
	}
}

func TestPDFCMapHelpers(t *testing.T) {
	if _, ok := pdfHexValue(nil); ok {
		t.Fatal("empty hex value must fail")
	}
	if _, ok := pdfHexValue([]byte("123456789")); ok {
		t.Fatal("overlong hex value must fail")
	}
	if got, ok := pdfHexValue([]byte("aF")); !ok || got != 0xAF {
		t.Fatalf("hex value = %x %v", got, ok)
	}
	if got := pdfCMapCodeLen([]byte("no ranges")); got != 1 {
		t.Fatalf("default code len = %d", got)
	}
	if got := pdfCMapCodeLen([]byte("begincodespacerange endcodespacerange")); got != 1 {
		t.Fatalf("tokenless code len = %d", got)
	}
	if got := pdfCMapSections([]byte("beginbfchar <01> <41>"), "bfchar"); len(got) != 0 {
		t.Fatalf("unterminated section = %d", len(got))
	}
}

func TestPDFCMapUnicodeValidation(t *testing.T) {
	for digits, expected := range map[string]string{
		"004":      "@",
		"00410042": "AB",
		"D83DDE00": "😀",
	} {
		text, valid := pdfCMapUnicodeText([]byte(digits))
		if !valid || text != expected {
			t.Fatalf("CMap text %q = %q/%t", digits, text, valid)
		}
	}
	for _, digits := range []string{"", "0", "zz", "D800", "D8000041", "DC00"} {
		if text, valid := pdfCMapUnicodeText([]byte(digits)); valid || text != "" {
			t.Fatalf("invalid CMap text %q = %q/%t", digits, text, valid)
		}
	}
	if text, valid := pdfIncrementedCMapText("A", 1); !valid || text != "B" {
		t.Fatalf("incremented CMap text = %q/%t", text, valid)
	}
	for text, delta := range map[string]uint32{"": 1, "\uD7FF": 1, "\U0010FFFF": 1} {
		if incremented, valid := pdfIncrementedCMapText(text, delta); valid || incremented != "" {
			t.Fatalf("invalid increment %q/%d = %q/%t", text, delta, incremented, valid)
		}
	}
}

func TestPDFCMapUnicodeRangeValidation(t *testing.T) {
	invalid := pdfParseCMapWithQuota([]byte(
		"beginbfchar\n<41> <D800>\n<42> <0042>\nendbfchar\n"+
			"beginbfrange\n<43> <45> <D7FF>\nendbfrange\n",
	), newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes))
	for code, want := range map[uint32]string{'A': "", 'B': "B", 'C': "\uD7FF", 'D': "", 'E': ""} {
		if text, _ := invalid.codeText(code); text != want {
			t.Fatalf("validated CMap %q = %q, want %q", rune(code), text, want)
		}
	}
	quota := newPDFCMapQuota(8, 64)
	bounded := pdfParseCMapWithQuota(
		[]byte("beginbfrange\n<0000> <FFFFFFFF> <D7FF>\nendbfrange\n"),
		quota,
	)
	if text, _ := bounded.codeText(0x2801); text != "\U00010000" || quota.remainingEntries != 7 {
		t.Fatalf("wide range = %q, %d entries left", text, quota.remainingEntries)
	}
	if text, mapped := bounded.codeText(1); mapped {
		t.Fatalf("surrogate step of a wide range mapped to %q", text)
	}
	tables := pdfTextTablesWithQuota(
		subsetFontPDF(t, "beginbfchar\n<02> <D800>\nendbfchar\n"),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if table := tables["F1"]; table == nil || shownFontText([]byte{0x02}, table) != "X" {
		t.Fatalf("invalid ToUnicode fallback = %#v", table)
	}
}

func TestPDFShownFontText(t *testing.T) {
	single := &pdfCMap{codeLen: 1, text: map[uint32]string{0x02: "fi"}}
	if got := shownFontText([]byte{0x02, 'r', 'm'}, single); got != "firm" {
		t.Fatalf("single-byte map = %q", got)
	}
	double := &pdfCMap{codeLen: 2, text: map[uint32]string{0x0041: "Ы"}}
	if got := shownFontText([]byte{0x00, 0x41, 'Z'}, double); got != "ЫZ" {
		t.Fatalf("double-byte map = %q", got)
	}
}

func TestPDFNameToken(t *testing.T) {
	name, consumed := pdfName([]byte("/F1.2_x 10 Tf"))
	if name != "F1.2_x" || consumed != len("/F1.2_x")-1 {
		t.Fatalf("name = %q %d", name, consumed)
	}
	if name, _ := pdfName([]byte("/")); name != "" {
		t.Fatalf("bare slash = %q", name)
	}
}

func shownFontText(raw []byte, table *pdfCMap) string {
	collector := newPDFTextCollector(pdfMaxTextBytes)
	pdfWriteShownText(collector, raw, table)

	return collector.String()
}
