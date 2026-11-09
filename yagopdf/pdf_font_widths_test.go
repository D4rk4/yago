package yagopdf

import (
	"fmt"
	"strings"
	"testing"
)

func fontWidthTable(definition, extra string) (*pdfCMap, *pdfFontResources) {
	body := []byte("1 0 obj\n<< /Type /Font " + definition + " >>\nendobj\n" + extra)
	fonts := newPDFFontResources(
		newPDFObjectLookup(body),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)

	return fonts.tables([]byte("<< /Font << /F1 1 0 R >> >>"))["F1"], fonts
}

func TestPDFSimpleFontGlyphWidths(t *testing.T) {
	for _, test := range []struct {
		name       string
		definition string
		extra      string
	}{
		{"direct", "/FirstChar 65 /LastChar 67 /Widths [250 0 750.5] " +
			"/FontDescriptor << /MissingWidth 125 >>", ""},
		{
			"indirect", "/FirstChar 2 0 R /LastChar 67 /Widths 3 0 R /FontDescriptor 4 0 R",
			"2 0 obj\n65\nendobj\n3 0 obj\n[250 0 750.5]\nendobj\n" +
				"4 0 obj\n<< /MissingWidth 5 0 R >>\nendobj\n5 0 obj\n125\nendobj\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			table, _ := fontWidthTable(test.definition, test.extra)
			for code, want := range map[uint32]float64{64: 125, 65: 250, 66: 0, 67: 750.5, 68: 125} {
				if got, known := table.widths.width(code); !known || got != want {
					t.Fatalf("width %d = %v, %t; want %v", code, got, known, want)
				}
			}
		})
	}
	table, _ := fontWidthTable("/FirstChar 255 /Widths [321]", "")
	if width, known := table.widths.width(255); !known || width != 321 {
		t.Fatalf("last simple font code width = %v, %t", width, known)
	}
	if _, known := table.widths.width(254); known {
		t.Fatal("unspecified simple font width became known")
	}
	var unavailable *pdfGlyphWidths
	if _, known := unavailable.width(65); known {
		t.Fatal("nil widths became known")
	}
}

func TestPDFSimpleFontWidthsRefuseMalformedArrays(t *testing.T) {
	for _, definition := range []string{
		"", "/FirstChar 65", "/Widths [500]", "/FirstChar -1 /Widths [500]",
		"/FirstChar 256 /Widths [500]", "/FirstChar 0.5 /Widths [500]",
		"/FirstChar 65 /Widths 99 0 R", "/FirstChar 65 /Widths /NotArray",
		"/FirstChar 65 /Widths [500 /Unknown]", "/FirstChar 65 /Widths [500",
		"/FirstChar 65 /Widths []", "/FirstChar 255 /Widths [500 500]",
		"/FirstChar 65 /LastChar 65 /Widths [500 500]",
		"/FirstChar 65 /LastChar /Unknown /Widths [500]",
		"/Subtype /Type3 /FirstChar 65 /Widths [500]",
	} {
		t.Run(definition, func(t *testing.T) {
			table, _ := fontWidthTable(definition, "")
			if table == nil {
				return
			}
			if _, known := table.widths.width(65); known {
				t.Fatalf("malformed width accepted: %s", definition)
			}
		})
	}
}

func TestPDFCIDFontWidthsAndDefaults(t *testing.T) {
	for _, test := range []struct {
		definition string
		widths     string
		want       map[uint32]float64
	}{
		{"", "", map[uint32]float64{0: 1000, 65535: 1000}},
		{"/DW 600", "", map[uint32]float64{0: 600, 65535: 600}},
		{
			"/DW 600 /W [1 [250 500.5] 10 20 750]", "",
			map[uint32]float64{0: 600, 1: 250, 2: 500.5, 3: 600, 10: 750, 20: 750, 21: 600},
		},
		{
			"/W 3 0 R", "3 0 obj\n[0 65535 321]\nendobj\n",
			map[uint32]float64{0: 321, 12345: 321, 65535: 321},
		},
		{"/W []", "", map[uint32]float64{65: 1000}},
	} {
		t.Run(test.definition, func(t *testing.T) {
			table, fonts := fontWidthTable(
				"/Subtype /Type0 /Encoding /Identity-H /DescendantFonts [2 0 R]",
				"2 0 obj\n<< "+test.definition+" >>\nendobj\n"+test.widths,
			)
			if table.codeLen != 2 {
				t.Fatalf("CID code length = %d", table.codeLen)
			}
			for code, want := range test.want {
				if width, known := table.widths.width(code); !known || width != want {
					t.Fatalf("width %d = %v, %t; want %v", code, width, known, want)
				}
			}
			if test.widths != "" && (len(table.widths.ranges) != 1 ||
				fonts.widthBudget.remainingEntries != pdfMaxGlyphWidthEntries-1) {
				t.Fatal("CID range expanded into individual entries")
			}
		})
	}
}

func TestPDFCIDFontWidthsRefuseMalformedArrays(t *testing.T) {
	for _, widths := range []string{
		"/W /Unknown", "/W [1]", "/W [1 2]", "/W [2 1 500]", "/W [-1 5 500]",
		"/W [1 65536 500]", "/W [1 2 /Unknown]", "/W [1 []]", "/W [1 [500 /Unknown]]",
		"/W [1 [500]", "/W [65535 [500 500]]", "/W [1.5 2 500]", "/DW /Unknown", "/W 99 0 R", "/DW 99 0 R",
	} {
		t.Run(widths, func(t *testing.T) {
			table, _ := fontWidthTable(
				"/Subtype /Type0 /Encoding /Identity-H /DescendantFonts [2 0 R]",
				"2 0 obj\n<< "+widths+" >>\nendobj\n",
			)
			if table == nil {
				return
			}
			if _, known := table.widths.width(1); known {
				t.Fatalf("malformed CID widths accepted: %s", widths)
			}
		})
	}
	for _, definition := range []string{
		"/Encoding /Identity-H", "/Encoding /Identity-H /DescendantFonts [99 0 R]",
		"/Encoding /Identity-H /DescendantFonts [2 0 R 3 0 R]",
		"/Encoding /Identity-H /DescendantFonts /Unknown",
		"/Encoding /Identity-V /DescendantFonts [2 0 R]",
		"/Encoding /UniJIS-UCS2-H /DescendantFonts [2 0 R]",
	} {
		table, _ := fontWidthTable("/Subtype /Type0 "+definition, "2 0 obj\n<< >>\nendobj\n")
		if _, known := table.widths.width(1); known {
			t.Fatalf("unresolved horizontal CID widths accepted: %s", definition)
		}
	}
}

func TestPDFFontWidthsShareBudgetAndCacheRefusals(t *testing.T) {
	for _, array := range []string{"[250]", "[250 500]", "[250 /Unknown]"} {
		body := []byte("1 0 obj\n<< /Type /Font /FirstChar 65 /Widths 4 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Font /FirstChar 66 /Widths 4 0 R >>\nendobj\n" +
			"3 0 obj\n<< /Type /Font /FirstChar 65 /Widths [900] >>\nendobj\n" +
			"4 0 obj\n" + array + "\nendobj\n")
		fonts := newPDFFontResources(
			newPDFObjectLookup(body),
			newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
		)
		fonts.widthBudget.remainingEntries = 1
		for object := 1; object <= 3; object++ {
			table := fonts.tables([]byte(fmt.Sprintf("<< /Font << /F1 %d 0 R >> >>", object)))["F1"]
			code := uint32(65)
			if object == 2 {
				code = 66
			}
			width, known := table.widths.width(code)
			wantKnown := array == "[250]" && object != 3
			if known != wantKnown || known && width != 250 {
				t.Fatalf("array %q font %d width = %v, %t", array, object, width, known)
			}
		}
		if fonts.widthBudget.remainingEntries != 0 {
			t.Fatal("width quota was not consumed")
		}
	}
}

func TestPDFFontWidthsBoundIndirectObjects(t *testing.T) {
	for _, size := range []int{128, pdfMaxGlyphWidthObjectBytes + 1} {
		table, fonts := fontWidthTable("/FirstChar 65 /Widths 2 0 R",
			"2 0 obj\n[500]"+strings.Repeat(" ", size)+"\nendobj\n")
		_, known := table.widths.width(65)
		if known != (size < pdfMaxGlyphWidthObjectBytes) {
			t.Fatalf("width object with %d padding bytes accepted = %t", size, known)
		}
		remaining := fonts.quota.remainingBytes
		fonts.chargedObject("2 0")
		if fonts.quota.remainingBytes != remaining {
			t.Fatal("indirect width object was charged twice")
		}
	}
}

func TestPDFCIDWidthsCacheBothSuccessfulAndRefusedArrays(t *testing.T) {
	for _, value := range []string{"[1 3 500]", "[1 3 /Unknown]"} {
		pdf := newSyntheticPDF().
			indirectObject(1, "<< /Type /Font /Subtype /Type0 /Encoding /Identity-H /DescendantFonts [3 0 R] >>").
			indirectObject(2, "<< /Type /Font /Subtype /Type0 /Encoding /Identity-H /DescendantFonts [3 0 R] >>").
			indirectObject(3, "<< /W 4 0 R >>").indirectObject(4, value)
		fonts := newPDFFontResources(
			newPDFObjectLookup(pdf.finished()),
			newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
		)
		fonts.widthBudget.remainingEntries = 1
		first := fonts.tables([]byte("<< /Font << /F1 1 0 R >> >>"))["F1"]
		remaining := fonts.quota.remainingBytes
		second := fonts.tables([]byte("<< /Font << /F1 2 0 R >> >>"))["F1"]
		for _, table := range []*pdfCMap{first, second} {
			width, known := table.widths.width(2)
			if known != (value == "[1 3 500]") || known && width != 500 {
				t.Fatalf("cached CID widths %q = %v, %t", value, width, known)
			}
		}
		if fonts.quota.remainingBytes != remaining-len(fonts.lookup.value("2 0")) {
			t.Fatal("cached CID descendant was charged twice")
		}
	}
}

func TestPDFGlyphWidthQuotaRefusesPartialResults(t *testing.T) {
	fonts := newPDFFontResources(
		newPDFObjectLookup(nil),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	fonts.widthBudget.remainingEntries = 1
	if ranges := fonts.cidWidthArray([]byte("[1 2 400 3 4 500]")); ranges != nil {
		t.Fatalf("quota-limited CID widths returned partial ranges: %#v", ranges)
	}
	for _, value := range []string{"[1 2 400]", "[400]"} {
		fonts = newPDFFontResources(newPDFObjectLookup(nil), newPDFDecodeQuota(0))
		fonts.lookup.byReference["2 0"] = pdfIndirectObject{value: []byte(value)}
		if widths := fonts.chargedObject("2 0"); widths != nil {
			t.Fatalf("zero decode quota admitted width object %q", value)
		}
	}
}
