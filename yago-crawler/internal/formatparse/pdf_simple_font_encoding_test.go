package formatparse

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func pdfSimpleEncodingFixture(
	t *testing.T,
	fontObjects string,
	fontReferences string,
	content string,
) []byte {
	t.Helper()
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.7\n")
	pdf.WriteString("1 0 obj\n<< /Type /Page /Resources << /Font <<\n")
	pdf.WriteString(fontReferences)
	pdf.WriteString(">> >> /Contents 2 0 R >>\nendobj\n")
	writePDFStreamObject(t, &pdf, 2, "", content)
	pdf.WriteString(fontObjects)
	pdf.WriteString("%%EOF")

	return pdf.Bytes()
}

func TestPDFSimpleFontControlGlyphsAndLayoutSpaces(t *testing.T) {
	body := pdfSimpleEncodingFixture(
		t,
		"5 0 obj\n<< /Type /Font /Encoding "+
			"<< /BaseEncoding /WinAnsiEncoding "+
			"/Differences [8 /C /l /e 12 /a /n] >> >>\nendobj\n"+
			"6 0 obj\n<< /Type /Font /Encoding "+
			"<< /Differences [8 /W /r /o 12 /n /g] >> >>\nendobj\n",
		"/F#31 5 0 R\n/F2 6 0 R\n",
		`BT /F#31 12 Tf (\b\t\n\f\r  Layout  phrase) Tj `+
			`/F2 (\b\t\n\f\r) Tj /Missing 12 Tf (plausible noise) Tj ET`,
	)
	page, parsed := parsePDF("https://example.test/layout.pdf", "application/pdf", body)
	if !parsed || !strings.Contains(page.Text, "Clean Layout phrase Clean") {
		t.Fatalf("simple font text = %t %q", parsed, page.Text)
	}
	for _, unwanted := range []string{"Wrong", "plausible noise", "  "} {
		if strings.Contains(page.Text, unwanted) {
			t.Fatalf("simple font text contains %q in %q", unwanted, page.Text)
		}
	}
}

func pdfEncodingSourceLookup() pdfObjectLookup {
	return newPDFObjectLookup([]byte(
		"7 0 obj\n<< /BaseEncoding /WinAnsiEncoding " +
			"/Differences [65 /Z 66 /.notdef 67 /UnknownGlyph] >>\nendobj\n",
	))
}

func TestPDFSimpleFontEncodingSources(t *testing.T) {
	lookup := pdfEncodingSourceLookup()
	directName := pdfSimpleFontEncodingTable(
		[]byte("<< /Encoding /WinAnsiEncoding >>"),
		lookup,
	)
	if directName == nil || directName.text['A'] != "A" || directName.text[0x80] != "€" {
		t.Fatalf("direct encoding = %#v", directName)
	}
	directDictionary := pdfSimpleFontEncodingTable(
		[]byte("<< /Encoding << /BaseEncoding /StandardEncoding "+
			"/Differences [65 /A#2Ealt 66 /.notdef 67 /UnknownGlyph] >> >>"),
		lookup,
	)
	if directDictionary == nil || directDictionary.text['A'] != "A" {
		t.Fatalf("dictionary encoding = %#v", directDictionary)
	}
	for _, code := range []uint32{'B', 'C'} {
		if _, exists := directDictionary.text[code]; exists {
			t.Fatalf("unknown difference retained code %d", code)
		}
	}
	indirect := pdfSimpleFontEncodingTable([]byte("<< /Encoding 7 0 R >>"), lookup)
	if indirect == nil || indirect.text['A'] != "Z" {
		t.Fatalf("indirect encoding = %#v", indirect)
	}
	for _, code := range []uint32{'B', 'C'} {
		if _, exists := indirect.text[code]; exists {
			t.Fatalf("indirect unknown difference retained code %d", code)
		}
	}
	differencesOnly := pdfSimpleFontEncodingTable(
		[]byte("<< /Encoding << /Differences [1 /A] >> >>"),
		lookup,
	)
	if differencesOnly == nil || differencesOnly.text[1] != "A" {
		t.Fatalf("differences-only encoding = %#v", differencesOnly)
	}
}

func TestPDFSimpleFontEncodingFailureModes(t *testing.T) {
	lookup := pdfEncodingSourceLookup()
	for _, font := range [][]byte{
		nil,
		[]byte("<< /Encoding 99 0 R >>"),
		[]byte("<< /Encoding << >> >>"),
		[]byte("<< /Encoding << /BaseEncoding /WinAnsiEncoding /Differences broken >> >>"),
	} {
		if table := pdfSimpleFontEncodingTable(font, lookup); table != nil {
			t.Fatalf("untrusted encoding = %#v for %q", table, font)
		}
	}
	huge := append([]byte("<< /Encoding << /BaseEncoding /WinAnsiEncoding "),
		bytes.Repeat([]byte{' '}, pdfMaxEncodingDictionaryBytes+1)...)
	huge = append(huge, []byte(">> >>")...)
	if table := pdfSimpleFontEncodingTable(huge, lookup); table != nil {
		t.Fatalf("oversized encoding = %#v", table)
	}
	unsupported := pdfSimpleFontEncodingTable([]byte("<< /Encoding /UnknownEncoding >>"), lookup)
	if unsupported == nil || len(unsupported.text) != 0 || !unsupported.omitUnmapped {
		t.Fatalf("unsupported encoding = %#v", unsupported)
	}
}

func TestPDFBaseEncodingFallbacks(t *testing.T) {
	standard := pdfBaseEncodingTable("StandardEncoding")
	for code, name := range map[uint32]string{
		0x27: "quoteright", 0x60: "quoteleft", 0xA1: "exclamdown", 0xA2: "cent",
		0xA3: "sterling", 0xA4: "fraction", 0xA5: "yen", 0xA6: "florin",
		0xA7: "section", 0xA8: "currency", 0xA9: "quotesingle", 0xAA: "quotedblleft",
		0xAB: "guillemotleft", 0xAC: "guilsinglleft", 0xAD: "guilsinglright",
		0xAE: "fi", 0xAF: "fl", 0xB1: "endash", 0xB2: "dagger",
		0xB3: "daggerdbl", 0xB4: "periodcentered", 0xB6: "paragraph", 0xB7: "bullet",
		0xB8: "quotesinglbase", 0xB9: "quotedblbase", 0xBA: "quotedblright",
		0xBB: "guillemotright", 0xBC: "ellipsis", 0xBD: "perthousand",
		0xBF: "questiondown", 0xC1: "grave", 0xC2: "acute", 0xC3: "circumflex",
		0xC4: "tilde", 0xC5: "macron", 0xC6: "breve", 0xC7: "dotaccent",
		0xC8: "dieresis", 0xCA: "ring", 0xCB: "cedilla", 0xCD: "hungarumlaut",
		0xCE: "ogonek", 0xCF: "caron", 0xD0: "emdash", 0xE1: "AE",
		0xE3: "ordfeminine", 0xE8: "Lslash", 0xE9: "Oslash", 0xEA: "OE",
		0xEB: "ordmasculine", 0xF1: "ae", 0xF5: "dotlessi", 0xF8: "lslash",
		0xF9: "oslash", 0xFA: "oe", 0xFB: "germandbls",
	} {
		if standard[code] != pdfGlyphNameText(name) {
			t.Fatalf("standard code %x = %q", code, standard[code])
		}
	}
	if len(standard) != 149 || standard['A'] != "A" || standard[0xA0] != "" {
		t.Fatalf("standard table = %d entries", len(standard))
	}
	macExpected := strings.Fields(`
		Adieresis Aring Ccedilla Eacute Ntilde Odieresis Udieresis aacute
		agrave acircumflex adieresis atilde aring ccedilla eacute egrave
		ecircumflex edieresis iacute igrave icircumflex idieresis ntilde oacute
		ograve ocircumflex odieresis otilde uacute ugrave ucircumflex udieresis
		dagger degree cent sterling section bullet paragraph germandbls
		registered copyright trademark acute dieresis .notdef AE Oslash
		.notdef plusminus .notdef .notdef yen mu .notdef .notdef
		.notdef .notdef .notdef ordfeminine ordmasculine .notdef ae oslash
		questiondown exclamdown logicalnot .notdef florin .notdef .notdef guillemotleft
		guillemotright ellipsis space Agrave Atilde Otilde OE oe
		endash emdash quotedblleft quotedblright quoteleft quoteright divide .notdef
		ydieresis Ydieresis fraction currency guilsinglleft guilsinglright fi fl
		daggerdbl periodcentered quotesinglbase quotedblbase perthousand Acircumflex Ecircumflex Aacute
		Edieresis Egrave Iacute Icircumflex Idieresis Igrave Oacute Ocircumflex
		.notdef Ograve Uacute Ucircumflex Ugrave dotlessi circumflex tilde
		macron breve dotaccent ring cedilla hungarumlaut ogonek caron
	`)
	mac := pdfBaseEncodingTable("MacRomanEncoding")
	if len(macExpected) != 128 || len(mac) != 208 || mac['A'] != "A" {
		t.Fatalf("mac table = %d names/%d entries", len(macExpected), len(mac))
	}
	for offset, name := range macExpected {
		if mac[uint32(0x80+offset)] != pdfGlyphNameText(name) {
			t.Fatalf("mac code %x = %q", 0x80+offset, mac[uint32(0x80+offset)])
		}
	}
}

func TestPDFEncodingDifferencesSequence(t *testing.T) {
	differences, valid := pdfEncodingDifferences(
		[]byte("[1 /A 999 /B -1 /C 2 /D +3 /E % note\n 4 /F @]"),
	)
	if !valid || len(differences) != 4 {
		t.Fatalf("differences = %#v/%t", differences, valid)
	}
	for index, expected := range []pdfEncodingDifference{
		{code: 1, text: "A"},
		{code: 2, text: "D"},
		{code: 3, text: "E"},
		{code: 4, text: "F"},
	} {
		if differences[index] != expected {
			t.Fatalf("difference %d = %#v", index, differences[index])
		}
	}
	if parsed, ok := pdfEncodingDifferences([]byte("[1 /A   ]")); !ok || len(parsed) != 1 {
		t.Fatalf("trailing-space differences = %#v/%t", parsed, ok)
	}
}

func TestPDFEncodingDifferencesRejectMalformed(t *testing.T) {
	for _, malformed := range [][]byte{nil, []byte("value"), []byte("[1 /A")} {
		if parsed, ok := pdfEncodingDifferences(malformed); ok || parsed != nil {
			t.Fatalf("malformed differences = %#v/%t", parsed, ok)
		}
	}
	for _, malformed := range []string{"[1 @ /A]", "[1.5 /A]", "[1foo /A]", "[/]", "[(]"} {
		if parsed, ok := pdfEncodingDifferences([]byte(malformed)); !ok || len(parsed) != 0 {
			t.Fatalf("malformed token %q = %#v/%t", malformed, parsed, ok)
		}
	}
	longName := "[1 /" + strings.Repeat("A", pdfMaxGlyphNameBytes+1) + " 2 /B]"
	parsed, ok := pdfEncodingDifferences([]byte(longName))
	if !ok || len(parsed) != 2 || parsed[0] != (pdfEncodingDifference{code: 1}) ||
		parsed[1] != (pdfEncodingDifference{code: 2, text: "B"}) {
		t.Fatalf("bounded glyph name = %#v/%t", parsed, ok)
	}
}

func TestPDFEncodingDifferencesBounded(t *testing.T) {
	var capped strings.Builder
	capped.WriteString("[0")
	for range 300 {
		capped.WriteString(" /A")
	}
	capped.WriteByte(']')
	if parsed, ok := pdfEncodingDifferences([]byte(capped.String())); !ok || len(parsed) != 256 {
		t.Fatalf("capped differences = %d/%t", len(parsed), ok)
	}
}

func TestPDFEncodingCodeSyntax(t *testing.T) {
	for _, testCase := range []struct {
		value    string
		code     uint32
		consumed int
		valid    bool
	}{
		{"", 0, 0, false},
		{"+", 0, 1, false},
		{"+12x", 12, 4, false},
		{"-1", 1, 2, false},
		{"256", 256, 3, false},
		{"999999999999x", 999, 13, false},
	} {
		code, consumed, ok := pdfEncodingCode([]byte(testCase.value))
		if code != testCase.code || consumed != testCase.consumed || ok != testCase.valid {
			t.Fatalf("encoding code %q = %d/%d/%t", testCase.value, code, consumed, ok)
		}
	}
}

func TestPDFGlyphNameAlgorithms(t *testing.T) {
	for name, expected := range map[string]string{
		"A.alt":            "A",
		"A_B":              "AB",
		"uni00410042":      "AB",
		"u1F600":           "😀",
		"ff_ffi_ffl":       "ﬀﬃﬄ",
		"Lcommaaccent":     "Ļ",
		"lcommaaccent":     "ļ",
		"uni0041_u000042":  "AB",
		"nonbreakingspace": " ",
		"mu":               "µ",
	} {
		if got := pdfGlyphNameText(name); got != expected {
			t.Fatalf("glyph %q = %q", name, got)
		}
	}
	for _, name := range []string{
		".notdef", "UnknownGlyph", "uni", "uni004", "uni00e9", "uniD800",
		"u123", "u1234567", "u00e9", "uD800", "u110000",
	} {
		if got := pdfGlyphNameText(name); got != "" {
			t.Fatalf("invalid glyph %q = %q", name, got)
		}
	}
	if !pdfUpperHex("") || pdfUnicodeScalar(0xD800) || pdfUnicodeScalar(utf8.MaxRune+1) {
		t.Fatal("glyph scalar validation failed")
	}
}

func TestPDFNameSyntax(t *testing.T) {
	for input, expected := range map[string]string{
		"/F#31 ": "F1",
		"/A#GG ": "A#GG",
		"/A#4 ":  "A#4",
	} {
		name, consumed, valid := pdfDecodedNameToken([]byte(input), pdfMaxPDFNameBytes)
		if !valid || name != expected || consumed != len(input)-1 {
			t.Fatalf("name %q = %q/%d/%t", input, name, consumed, valid)
		}
	}
	for _, input := range [][]byte{nil, []byte("name"), []byte("/"), []byte("//")} {
		if name, _, valid := pdfDecodedNameToken(input, pdfMaxPDFNameBytes); valid || name != "" {
			t.Fatalf("invalid name %q = %q/%t", input, name, valid)
		}
	}
	if name, _, valid := pdfDecodedNameToken([]byte("/A"), 0); valid || name != "" {
		t.Fatalf("zero-limit name = %q/%t", name, valid)
	}
	tooLong := append([]byte{'/'}, bytes.Repeat([]byte{'A'}, pdfMaxPDFNameBytes+1)...)
	if name, consumed, valid := pdfDecodedNameToken(tooLong, pdfMaxPDFNameBytes); valid ||
		name != "" || consumed != len(tooLong) {
		t.Fatalf("long name = %q/%d/%t", name, consumed, valid)
	}
	for input, expected := range map[string]byte{"0": 0, "A": 10, "f": 15} {
		value, valid := pdfHexNibble(input[0])
		if !valid || value != expected {
			t.Fatalf("nibble %q = %d/%t", input, value, valid)
		}
	}
	if _, valid := pdfHexNibble('x'); valid {
		t.Fatal("invalid nibble accepted")
	}
}

func TestPDFFontSelectionSyntax(t *testing.T) {
	for _, input := range []string{"/F1 12 Tf ", "/F#31 -1.25 Tf/", "/F1 +12. Tf"} {
		name, consumed, valid := pdfFontSelection([]byte(input))
		if !valid || name != "F1" || consumed <= len(name) {
			t.Fatalf("font selection %q = %q/%d/%t", input, name, consumed, valid)
		}
	}
	for _, input := range []string{
		"", "/", "/F1 Tf", "/F1 . Tf", "/F1 12", "/F1 12 Tj", "/F1 12 TfX", "/F1 1.2.3 Tf",
	} {
		if name, _, valid := pdfFontSelection([]byte(input)); valid {
			t.Fatalf("invalid font selection %q = %q/%t", input, name, valid)
		}
	}
	if end, valid := pdfFontSizeEnd([]byte("12"), 0); end != 2 || !valid {
		t.Fatalf("terminal font size = %d/%t", end, valid)
	}
}

func TestPDFUnknownSelectedFontsFailClosed(t *testing.T) {
	for name, font := range map[string]string{
		"direct":     "<< /Type /Font /Subtype /Type1 /Encoding /WinAnsiEncoding >>",
		"unresolved": "99 0 R",
	} {
		t.Run(name, func(t *testing.T) {
			var pdf bytes.Buffer
			pdf.WriteString("%PDF-1.7\n1 0 obj\n<< /Type /Page /Resources << /Font << /F1 ")
			pdf.WriteString(font)
			pdf.WriteString(" >> >> /Contents 2 0 R >>\nendobj\n")
			writePDFStreamObject(t, &pdf, 2, "", "BT /F1 12 Tf (plausible raw glyph noise) Tj ET")
			pdf.WriteString("%%EOF")
			page, parsed := parsePDF(
				"https://example.test/unknown-font.pdf",
				"application/pdf",
				pdf.Bytes(),
			)
			if parsed || page.Text != "" {
				t.Fatalf("unknown font parsed = %t %q", parsed, page.Text)
			}
		})
	}
}

func TestPDFSimpleFontEncodingSharesDecodeQuota(t *testing.T) {
	font := []byte("<< /Type /Font /Encoding /WinAnsiEncoding >>")
	tooSmall := newPDFDecodeQuota(len(font) - 1)
	if table := pdfSimpleFontEncodingTableWithQuota(
		font,
		pdfObjectLookup{},
		tooSmall,
	); table != nil ||
		!tooSmall.exhausted() {
		t.Fatalf("font quota = %#v/%d", table, tooSmall.remainingBytes)
	}
	exact := newPDFDecodeQuota(len(font))
	if table := pdfSimpleFontEncodingTableWithQuota(font, pdfObjectLookup{}, exact); table == nil ||
		table.text['A'] != "A" || !exact.exhausted() {
		t.Fatalf("exact font quota = %#v/%d", table, exact.remainingBytes)
	}
	lookup := pdfEncodingSourceLookup()
	indirectFont := []byte("<< /Type /Font /Encoding 7 0 R >>")
	indirectWork := len(indirectFont) + len(lookup.value("7 0"))
	indirectTooSmall := newPDFDecodeQuota(indirectWork - 1)
	if table := pdfSimpleFontEncodingTableWithQuota(
		indirectFont,
		lookup,
		indirectTooSmall,
	); table != nil || !indirectTooSmall.exhausted() {
		t.Fatalf("indirect quota = %#v/%d", table, indirectTooSmall.remainingBytes)
	}
	indirectExact := newPDFDecodeQuota(indirectWork)
	if table := pdfSimpleFontEncodingTableWithQuota(
		indirectFont,
		lookup,
		indirectExact,
	); table == nil || table.text['A'] != "Z" || !indirectExact.exhausted() {
		t.Fatalf("indirect exact quota = %#v/%d", table, indirectExact.remainingBytes)
	}
}

func TestPDFTextTableLayeringConflictAndCap(t *testing.T) {
	var fonts strings.Builder
	for index := 0; index < pdfMaxFontTables+8; index++ {
		fmt.Fprintf(&fonts, "/F%d %d 0 R\n", index, 100+index)
		fmt.Fprintf(
			&fonts,
			"%d 0 obj\n<< /Type /Font /Encoding /WinAnsiEncoding >>\nendobj\n",
			100+index,
		)
	}
	fonts.WriteString("/NotFont 900 0 R\n900 0 obj\n<< /Type /Page >>\nendobj\n")
	fonts.WriteString("/Conflict 901 0 R\n/Conflict 902 0 R\n")
	fonts.WriteString("901 0 obj\n<< /Type /Font /Encoding /WinAnsiEncoding >>\nendobj\n")
	fonts.WriteString("902 0 obj\n<< /Type /Font /Encoding /WinAnsiEncoding >>\nendobj\n")
	tables := pdfTextTablesWithQuota(
		[]byte(fonts.String()),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if len(tables) != pdfMaxFontTables || tables["F0"] == nil || tables["F63"] == nil ||
		tables["F64"] != nil || tables["NotFont"] != nil {
		t.Fatalf("capped tables = %d %#v", len(tables), tables)
	}
	conflictBody := []byte(
		"/Conflict 901 0 R\n/Conflict 902 0 R\n" +
			"901 0 obj\n<< /Type /Font >>\nendobj\n" +
			"902 0 obj\n<< /Type /Font >>\nendobj\n",
	)
	conflict := pdfTextTablesWithQuota(
		conflictBody,
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if table := conflict["Conflict"]; table == nil || !table.omitUnmapped || len(table.text) != 0 {
		t.Fatalf("conflict table = %#v", table)
	}
	plain := pdfTextTablesWithQuota(
		[]byte("/Plain 903 0 R\n903 0 obj\n<< /Type /Font >>\nendobj\n"),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if table := plain["Plain"]; table == nil || !table.omitUnmapped {
		t.Fatalf("unavailable table = %#v", table)
	}
	longName := "/" + strings.Repeat("A", pdfMaxPDFNameBytes+1) + " 904 0 R\n" +
		"904 0 obj\n<< /Type /Font >>\nendobj\n"
	if tables := pdfTextTablesWithQuota(
		[]byte(longName),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	); len(tables) != 0 {
		t.Fatalf("long resource name tables = %#v", tables)
	}
	if names, _ := pdfFontObjectReferences([]byte(longName)); len(names) != 0 {
		t.Fatalf("long generic font names = %#v", names)
	}
	oversized := append([]byte(
		"/F 1 0 R\n1 0 obj\n<< /Type /Font "+
			"/Encoding /WinAnsiEncoding >>\nendobj\n",
	),
		bytes.Repeat([]byte{' '}, pdfMaxObjScanBytes)...)
	if table := pdfTextTablesWithQuota(
		oversized,
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)["F"]; table == nil || table.text['A'] != "A" {
		t.Fatalf("oversized scan table = %#v", table)
	}
}

func TestPDFLiteralLineEndingsAreCanonicalBytes(t *testing.T) {
	for name, lineEnding := range map[string]string{"cr": "\r", "lf": "\n", "crlf": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			literal := []byte("(a" + lineEnding + "b)")
			raw, _ := pdfRawStringLiteral(literal)
			bounded, _ := pdfRawStringLiteralWithin(literal, len(literal))
			if string(raw) != "a\nb" || !bytes.Equal(raw, bounded) {
				t.Fatalf("line ending = %q/%q", raw, bounded)
			}
			continuation := []byte("(a\\" + lineEnding + "b)")
			raw, _ = pdfRawStringLiteral(continuation)
			bounded, _ = pdfRawStringLiteralWithin(continuation, len(continuation))
			if string(raw) != "ab" || !bytes.Equal(raw, bounded) {
				t.Fatalf("continuation = %q/%q", raw, bounded)
			}
		})
	}
	decoded := newPDFTextCollector(16)
	pdfWriteDecodedStringBytes(decoded, []byte{'a', '\t', '\n', '\r', '\b', '\f', 'b'})
	if decoded.String() != "a   b" {
		t.Fatalf("decoded controls = %q", decoded.String())
	}
	unmapped := newPDFTextCollector(4)
	pdfWriteShownText(unmapped, []byte{'A'}, &pdfCMap{codeLen: 1, text: map[uint32]string{}})
	if unmapped.String() != "A" {
		t.Fatalf("unmapped shown text = %q", unmapped.String())
	}
	oddSuppressed := newPDFTextCollector(4)
	pdfWriteShownText(
		oddSuppressed,
		[]byte{'A'},
		&pdfCMap{codeLen: 2, text: map[uint32]string{}, omitUnmapped: true},
	)
	if oddSuppressed.String() != "" {
		t.Fatalf("odd selected-font tail = %q", oddSuppressed.String())
	}
	oddFallback := newPDFTextCollector(8)
	pdfWriteShownText(
		oddFallback,
		[]byte{'A'},
		&pdfCMap{
			codeLen:      2,
			text:         map[uint32]string{},
			fallback:     &pdfCMap{codeLen: 1, text: map[uint32]string{'A': "mapped"}},
			omitUnmapped: true,
		},
	)
	if oddFallback.String() != "mapped" {
		t.Fatalf("odd selected-font fallback = %q", oddFallback.String())
	}
	if got := pdfMapString(
		[]byte{'A'},
		&pdfCMap{
			codeLen: 1,
			text:    map[uint32]string{},
			fallback: &pdfCMap{
				codeLen: 1,
				text:    map[uint32]string{'A': "mapped"},
			},
		},
	); got != "mapped" {
		t.Fatalf("fallback map text = %q", got)
	}
	if got := pdfMapString(
		[]byte{'A'},
		&pdfCMap{codeLen: 2, text: map[uint32]string{}, omitUnmapped: true},
	); got != "" {
		t.Fatalf("odd map tail = %q", got)
	}
	if got := pdfMapString(
		[]byte{'A'},
		&pdfCMap{
			codeLen:      2,
			text:         map[uint32]string{},
			fallback:     &pdfCMap{codeLen: 1, text: map[uint32]string{'A': "mapped"}},
			omitUnmapped: true,
		},
	); got != "mapped" {
		t.Fatalf("odd fallback map tail = %q", got)
	}
}
