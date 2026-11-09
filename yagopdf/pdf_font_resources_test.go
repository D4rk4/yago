package yagopdf

import (
	"fmt"
	"strings"
	"testing"
)

func TestPDFFontResourcesKeepNamesLocalToEachScope(t *testing.T) {
	body := newSyntheticPDF().
		indirectObject(1, "<< /Type /Font /Encoding << /Differences [65 /A] >> >>").
		indirectObject(2, "<< /Type /Font /Encoding << /Differences [65 /B] >> >>").
		indirectObject(3, "<< /Type /Font /Encoding << /Differences [65 /C] >> >>").
		indirectObject(4, "<< /F1 3 0 R >>").finished()
	fonts := newPDFFontResources(
		newPDFObjectLookup(body),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	first := []byte("<< /Font << /F1 1 0 R >> >>")
	second := []byte("<< /Font << /F1 2 0 R >> >>")
	form := []byte("<< /Font 4 0 R >>")
	for _, test := range []struct {
		resources []byte
		want      string
	}{{first, "A"}, {second, "B"}, {form, "C"}, {first, "A"}} {
		table := fonts.tables(test.resources)["F1"]
		if table == nil || shownFontText([]byte("A"), table) != test.want {
			t.Fatalf("scope %q = %#v, want %q", test.resources, table, test.want)
		}
	}
	if len(fonts.tables(nil)) != 0 || len(fonts.tables([]byte("<< >>"))) != 0 {
		t.Fatal("missing resources inherited an unrelated font")
	}
}

func TestPDFFontResourcesRejectNestedAndConflictingNames(t *testing.T) {
	body := newSyntheticPDF().
		indirectObject(1, "<< /Type /Font /Encoding /WinAnsiEncoding >>").
		indirectObject(2, "<< /Type /Font /Encoding /WinAnsiEncoding >>").finished()
	fonts := newPDFFontResources(
		newPDFObjectLookup(body),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	resources := []byte("<< /Font << /F#31 1 0 R /Nested << /Bad 2 0 R >> " +
		"/Note (/Bad 2 0 R) /Conflict 1 0 R /Conflict 2 0 R >> >>")
	tables := fonts.tables(resources)
	if len(tables) != 2 || shownFontText([]byte("A"), tables["F1"]) != "A" ||
		shownFontText([]byte("A"), tables["Conflict"]) != "" {
		t.Fatalf("scoped names = %#v", tables)
	}
}

func TestPDFFontResourcesReuseFontsUnicodeAndWidths(t *testing.T) {
	cmap := "1 beginbfchar\n<41> <0042>\nendbfchar"
	body := newSyntheticPDF().
		indirectObject(1, "<< /Type /Font /ToUnicode 4 0 R /FirstChar 65 /Widths 5 0 R >>").
		indirectObject(2, "<< /Type /Font /ToUnicode 4 0 R /FirstChar 66 /Widths 5 0 R >>").
		rawStream(4, "", cmap).
		indirectObject(5, "[321]").
		indirectObject(6, "<< /F1 1 0 R /F2 2 0 R >>").finished()
	quota := newPDFDecodeQuota(pdfMaxDecodedDocumentBytes)
	fonts := newPDFFontResources(newPDFObjectLookup(body), quota)
	resources := []byte("<< /Font 6 0 R >>")
	tables := fonts.tables(resources)
	for name, code := range map[string]uint32{"F1": 65, "F2": 66} {
		if shownFontText([]byte("A"), tables[name]) != "B" {
			t.Fatalf("ToUnicode text for %s", name)
		}
		if width, known := tables[name].widths.width(code); !known || width != 321 {
			t.Fatalf("width for %s = %v, %t", name, width, known)
		}
	}
	remaining := quota.remainingBytes
	reused := fonts.tables([]byte("<< /Font << /Other 1 0 R >> >>"))["Other"]
	if fonts.tables(resources)["F1"] != reused || reused != tables["F1"] ||
		fonts.tables([]byte("<< /Font 6 0 R >>"))["F2"] != tables["F2"] ||
		quota.remainingBytes != remaining || fonts.cmapQuota.remainingEntries != pdfMaxCMapEntries-1 ||
		fonts.widthBudget.remainingEntries != pdfMaxGlyphWidthEntries-1 {
		t.Fatal("font, CMap, width or resource cache consumed its budget twice")
	}
}

func TestPDFFontResourcesCacheRefusedFontsAndCMaps(t *testing.T) {
	body := newSyntheticPDF().
		indirectObject(1, "<< /Type /Other >>").
		indirectObject(2, "<< /Type /Font /ToUnicode 4 0 R >>").
		indirectObject(3, "<< /Type /Font /ToUnicode 4 0 R >>").
		rawStream(4, "", "begincmap endcmap").finished()
	fonts := newPDFFontResources(
		newPDFObjectLookup(body),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	firstCheck := fonts.isFont("1 0")
	if firstCheck || fonts.isFont("1 0") || len(fonts.fontObjects) != 1 {
		t.Fatal("nonfont object check was not cached")
	}
	fonts.tables([]byte("<< /Font << /F1 2 0 R >> >>"))
	remaining := fonts.quota.remainingBytes
	fonts.tables([]byte("<< /Font << /F1 3 0 R >> >>"))
	if len(fonts.unicodeMaps) != 1 || fonts.unicodeMaps["4 0"] != nil ||
		fonts.quota.remainingBytes != remaining-len(fonts.lookup.value("3 0")) {
		t.Fatal("refused ToUnicode stream was decoded twice")
	}
}

func TestPDFFontResourcesShareFontAndCMapLimitsAcrossScopes(t *testing.T) {
	pdf := newSyntheticPDF().rawStream(900, "", "1 beginbfchar\n<41> <0042>\nendbfchar")
	for object := 1; object <= pdfMaxFontTables+1; object++ {
		pdf.indirectObject(object, "<< /Type /Font /ToUnicode 900 0 R >>")
	}
	fonts := newPDFFontResources(newPDFObjectLookup(pdf.finished()),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes))
	for object := 1; object <= pdfMaxFontTables+1; object++ {
		resources := []byte(fmt.Sprintf("<< /Font << /F1 %d 0 R >> >>", object))
		text := shownFontText([]byte("A"), fonts.tables(resources)["F1"])
		if (text == "B") != (object <= pdfMaxFontTables) {
			t.Fatalf("font %d text = %q", object, text)
		}
	}
	if len(fonts.fonts) != pdfMaxFontTables || len(fonts.unicodeMaps) != 1 {
		t.Fatal("font cap or shared CMap cache reset per resource")
	}
	fonts = newPDFFontResources(newPDFObjectLookup(newSyntheticPDF().
		indirectObject(1, "<< /Type /Font /ToUnicode 4 0 R >>").
		indirectObject(2, "<< /Type /Font /ToUnicode 5 0 R >>").
		rawStream(4, "", "1 beginbfchar\n<41> <0042>\nendbfchar").
		rawStream(5, "", "1 beginbfchar\n<41> <0043>\nendbfchar").finished()),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes))
	fonts.cmapQuota = newPDFCMapQuota(1, pdfMaxCMapTextBytes)
	for object, want := range []string{"B", ""} {
		resources := []byte(fmt.Sprintf("<< /Font << /F1 %d 0 R >> >>", object+1))
		if text := shownFontText([]byte("A"), fonts.tables(resources)["F1"]); text != want {
			t.Fatalf("CMap quota on scope %d = %q, want %q", object, text, want)
		}
	}
}

func TestPDFFontResourceScopesFollowPageAndFormResources(t *testing.T) {
	pdf := newSyntheticPDF().
		indirectObject(10, "<< /Type /Font /Encoding << /Differences [65 /A] >> >>").
		indirectObject(11, "<< /Type /Font /Encoding << /Differences [65 /B] >> >>").
		indirectObject(12, "<< /Type /Font /Encoding << /Differences [65 /C] >> >>").
		indirectObject(20, "<< /Type /Pages /Resources << /Font << /F1 10 0 R >> >> >>").
		page(1, "/Parent 20 0 R /Contents 3 0 R").
		page(2, "/Resources << /Font << /F1 11 0 R >> /XObject << /Form 5 0 R >> >> /Contents 4 0 R").
		rawStream(3, "", "BT /F1 10 Tf (AAA) Tj ET").
		rawStream(4, "", "BT /F1 10 Tf (AAA) Tj ET").
		rawStream(5, "/Subtype /Form /Resources << /Font << /F1 12 0 R >> >>",
			"BT /F1 10 Tf (AAA) Tj ET")
	document, readable := ReadPDF(pdf.finished())
	if !readable || strings.Join(strings.Fields(document.Text), " ") != "AAA BBB CCC" {
		t.Fatalf("page/form scoped text = %q, %t", document.Text, readable)
	}
}

func TestPDFFontResourcesAcceptNonzeroObjectGenerations(t *testing.T) {
	body := []byte("1 3 obj\n<< /Type /Font /ToUnicode 2 4 R >>\nendobj\n" +
		"2 4 obj\n<< >>\nstream\n1 beginbfchar\n<41> <0042>\nendbfchar\nendstream\nendobj\n")
	fonts := newPDFFontResources(
		newPDFObjectLookup(body),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	table := fonts.tables([]byte("<< /Font << /F1 1 3 R >> >>"))["F1"]
	if table == nil || shownFontText([]byte("A"), table) != "B" {
		t.Fatalf("nonzero generation font = %#v", table)
	}
}
