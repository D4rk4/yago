package formatparse

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
	fmt.Fprintf(&pdf, "6 0 obj\n<< /Length %d >>\nstream\n", len(compressed))
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
	page, parsed := parsePDF("https://a.example/1706.03762", "application/pdf",
		subsetFontPDF(t, subsetCMap))
	if !parsed || !strings.Contains(page.Text, "firmly established") {
		t.Fatalf("subset font text = %v %q", parsed, page.Text)
	}
}

// TestPDFToUnicodeTableShapes pins the table builder's guardrails: conflicting
// font names drop, shared CMap objects parse once, fontless and cmapless
// bodies yield nothing, and the object scan cap holds.
func TestPDFToUnicodeTableShapes(t *testing.T) {
	body := subsetFontPDF(t, subsetCMap)
	tables := pdfToUnicodeTables(body)
	if tables["F1"] == nil || tables["F1"].text[0x02] != "fi" {
		t.Fatalf("tables = %+v", tables)
	}
	if tables["F1"].text[0x42] != "Б" {
		t.Fatalf("bfrange increment = %q", tables["F1"].text[0x42])
	}
	shared := append([]byte{}, body...)
	shared = append(shared, []byte("\n8 0 obj\n<< /Font << /F2 5 0 R >> >>\nendobj\n")...)
	sharedTables := pdfToUnicodeTables(shared)
	if sharedTables["F1"] == nil || sharedTables["F1"] != sharedTables["F2"] {
		t.Fatal("shared cmap object must reuse one table")
	}
	conflict := append([]byte{}, body...)
	conflict = append(conflict, []byte("\n/F1 9 0 R\n9 0 obj\n<< /ToUnicode 6 0 R >>\nendobj\n")...)
	if got := pdfToUnicodeTables(conflict); got["F1"] != nil {
		t.Fatalf("conflicting name must drop, got %+v", got["F1"])
	}
	if got := pdfToUnicodeTables([]byte("%PDF-1.5\nno fonts here")); len(got) != 0 {
		t.Fatalf("fontless pdf tables = %d", len(got))
	}
	if got := pdfToUnicodeTables([]byte("/F1 5 0 R\n5 0 obj\n<< /Type /Font >>\nendobj")); len(
		got,
	) != 0 {
		t.Fatalf("cmapless font tables = %d", len(got))
	}
	if got := pdfToUnicodeTables(subsetFontPDF(t, "begincmap endcmap")); len(got) != 0 {
		t.Fatalf("mapless cmap tables = %d", len(got))
	}
	huge := append(make([]byte, 0, pdfMaxObjScanBytes+16), body...)
	huge = append(huge, bytes.Repeat([]byte{' '}, pdfMaxObjScanBytes+16-len(huge))...)
	if got := pdfToUnicodeTables(huge); got["F1"] == nil {
		t.Fatal("oversized body must still scan its head")
	}
}

// TestPDFToUnicodeTableCap pins the font-table cap: names beyond the cap stay
// untabled instead of growing without bound.
func TestPDFToUnicodeTableCap(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	for i := 0; i < pdfMaxFontTables+8; i++ {
		fmt.Fprintf(&pdf, "/N%d %d 0 R\n", i, 100+i)
		fmt.Fprintf(&pdf, "%d 0 obj\n<< /ToUnicode 900 0 R >>\nendobj\n", 100+i)
	}
	compressed := zlibBytes(t, subsetCMap)
	fmt.Fprintf(&pdf, "900 0 obj\n<< /Length %d >>\nstream\n", len(compressed))
	pdf.Write(compressed)
	pdf.WriteString("\nendstream\nendobj\n")
	tables := pdfToUnicodeTables(pdf.Bytes())
	if len(tables) != pdfMaxFontTables {
		t.Fatalf("tables = %d, want %d", len(tables), pdfMaxFontTables)
	}
}

func TestPDFToUnicodeObjectLookupWorkIsLinear(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.7\n")
	for index := 0; index < pdfMaxIndirectObjects; index++ {
		object := 10_000 + index
		fmt.Fprintf(&pdf, "/F%d %d 0 R\n", index, object)
		fmt.Fprintf(&pdf, "%d 0 obj\n<< /Type /Font >>\nendobj\n", object)
	}
	body := pdf.Bytes()
	lookup := newPDFObjectLookup(body)
	fontNames, objectOf := pdfFontObjectReferences(body)
	reads := 0
	tables := pdfToUnicodeTablesFromObjects(
		fontNames,
		objectOf,
		func(reference string) []byte {
			reads++

			return lookup.value(reference)
		},
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if len(lookup.objects) != pdfMaxIndirectObjects ||
		len(fontNames) != pdfMaxIndirectObjects ||
		reads != pdfMaxIndirectObjects || len(tables) != 0 {
		t.Fatalf(
			"lookup work = %d objects/%d fonts/%d reads/%d tables",
			len(lookup.objects),
			len(fontNames),
			reads,
			len(tables),
		)
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
	if got := pdfObjectStream(lookup, "7"); got != nil {
		t.Fatalf("missing object stream = %q", got)
	}
	if got := pdfObjectStream(lookup, "6"); got != nil {
		t.Fatalf("streamless object = %q", got)
	}
	raw := []byte("8 0 obj\n<< >>\nstream\r\nplain\nendstream\nendobj")
	if got := pdfObjectStream(newPDFObjectLookup(raw), "8"); got != nil {
		t.Fatalf("non-flate stream = %q", got)
	}
	unterminated := []byte("9 0 obj\n<< >>\nstream\nabc")
	if got := pdfObjectStream(newPDFObjectLookup(unterminated), "9"); got != nil {
		t.Fatalf("unterminated stream = %q", got)
	}
	if got := pdfObjectStreamWithQuota(
		func(string) []byte { return []byte("<< >>\nstream\nunterminated") },
		"9",
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); got != nil {
		t.Fatalf("unterminated direct stream = %q", got)
	}
	fontNoDict := []byte("3 0 obj\nstream\n/ToUnicode 4 0 R\nendstream\nendobj")
	fontLookup := newPDFObjectLookup(fontNoDict)
	if got := pdfToUnicodeObjectOf(fontLookup.value, "3"); got != "" {
		t.Fatalf("stream-only tounicode = %q", got)
	}
	if got := pdfToUnicodeObjectOf(fontLookup.value, "4"); got != "" {
		t.Fatalf("missing font object = %q", got)
	}
}

// TestPDFCMapParsingEdges pins the CMap reader: nil and mapless sources yield
// no table, the array bfrange form and malformed rows skip, reversed ranges
// skip, the codespace width picks the code size, and the entry cap holds.
func TestPDFCMapParsingEdges(t *testing.T) {
	if pdfParseCMap(nil) != nil {
		t.Fatal("nil cmap must yield no table")
	}
	if pdfParseCMap([]byte("begincmap endcmap")) != nil {
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
	table := pdfParseCMap(src)
	if table == nil || table.codeLen != 2 {
		t.Fatalf("table = %+v", table)
	}
	if table.text[0x41] != "" || table.text[0x44] != "" || table.text[0x46] != "" {
		t.Fatalf("skipped rows leaked: %+v", table.text)
	}
	if table.text[0x45] != "W" {
		t.Fatalf("bfchar = %q", table.text[0x45])
	}
	var capped strings.Builder
	capped.WriteString("1 beginbfrange\n<0000> <FFFF> <0041>\nendbfrange\n")
	cappedTable := pdfParseCMap([]byte(capped.String()))
	if len(cappedTable.text) != pdfMaxCMapEntries {
		t.Fatalf("range cap = %d", len(cappedTable.text))
	}
	var charCapped strings.Builder
	charCapped.WriteString("beginbfchar\n")
	for i := 0; i < pdfMaxCMapEntries+8; i++ {
		fmt.Fprintf(&charCapped, "<%04X> <0041>\n", i)
	}
	charCapped.WriteString("endbfchar\n")
	charTable := pdfParseCMap([]byte(charCapped.String()))
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
	invalid := pdfParseCMap([]byte(
		"beginbfchar\n<41> <D800>\n<42> <0042>\nendbfchar\n" +
			"beginbfrange\n<43> <45> <D7FF>\nendbfrange\n",
	))
	if invalid == nil || invalid.text['A'] != "" || invalid.text['B'] != "B" ||
		invalid.text['C'] != "\uD7FF" || invalid.text['D'] != "" || invalid.text['E'] != "" {
		t.Fatalf("validated CMap = %#v", invalid)
	}
	quota := newPDFCMapQuota(8, 64)
	bounded := pdfParseCMapWithQuota(
		[]byte("beginbfrange\n<0000> <FFFFFFFF> <D7FF>\nendbfrange\n"),
		quota,
	)
	if bounded == nil || len(bounded.text) != 1 || quota.remainingEntries != 0 {
		t.Fatalf("invalid range bound = %#v/%d", bounded, quota.remainingEntries)
	}
	tables := pdfTextTablesWithQuota(
		subsetFontPDF(t, "beginbfchar\n<02> <D800>\nendbfchar\n"),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if table := tables["F1"]; table == nil || pdfMapString([]byte{0x02}, table) != "X" {
		t.Fatalf("invalid ToUnicode fallback = %#v", table)
	}
}

func TestPDFMapString(t *testing.T) {
	single := &pdfCMap{codeLen: 1, text: map[uint32]string{0x02: "fi"}}
	if got := pdfMapString([]byte{0x02, 'r', 'm'}, single); got != "firm" {
		t.Fatalf("single-byte map = %q", got)
	}
	double := &pdfCMap{codeLen: 2, text: map[uint32]string{0x0041: "Ы"}}
	if got := pdfMapString([]byte{0x00, 0x41, 'Z'}, double); got != "ЫZ" {
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
