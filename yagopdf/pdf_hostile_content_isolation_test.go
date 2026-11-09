package yagopdf

import (
	"testing"
)

const (
	hostileIsolatedPageText = "Legitimate isolated page words"
	hostileIsolatedPayload  = "Isolated payload words"
	hostileIsolatedContent  = "BT (" + hostileIsolatedPayload + ") Tj ET"
)

type isolationCase struct {
	name     string
	document []byte
	payload  bool
}

func isolatedPagePDF(pageEntries string, objects func(*syntheticPDF)) []byte {
	pdf := newSyntheticPDF().
		page(1, "/Contents 2 0 R "+pageEntries).
		rawStream(2, "", "BT ("+hostileIsolatedPageText+") Tj ET")
	objects(pdf)

	return pdf.finished()
}

func requireIsolationCases(t *testing.T, cases []isolationCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, testCase.document)
			requireTextContains(t, extraction.document.Text, hostileIsolatedPageText)
			if testCase.payload {
				requireTextContains(t, extraction.document.Text, hostileIsolatedPayload)

				return
			}
			requireTextOmits(t, extraction.document.Text, hostileIsolatedPayload)
		})
	}
}

func TestHostileTextOutsideTextBlocksIsNotExtracted(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	inside := "BT (" + hostileIsolatedPageText + ") Tj ET"
	payload := "(" + hostileIsolatedPayload + ")"
	formPDF := func(form string) []byte {
		return isolatedPagePDF(
			"/Resources << /XObject << /Fm1 3 0 R >> >>",
			func(pdf *syntheticPDF) {
				pdf.rawStream(3, "/Type /XObject /Subtype /Form /BBox [0 0 612 792]", form)
			},
		)
	}
	requireIsolationCases(t, []isolationCase{
		{"before the first block", singlePagePDF(payload + " Tj " + inside), false},
		{"between two blocks", singlePagePDF(inside + " " + payload + " Tj " + inside), false},
		{
			"after the last block with every show operator",
			singlePagePDF(
				inside + " " + payload + " Tj " + payload + " ' 1 2 " + payload + ` " [` + payload + "] TJ",
			),
			false,
		},
		{
			"hex string after the last block",
			singlePagePDF(inside + " <" + utf16BEHex(hostileIsolatedPayload) + "> Tj"),
			false,
		},
		{"inside a second block", singlePagePDF(inside + " BT " + payload + " Tj ET"), true},
		{
			"outside a block in a form",
			formPDF(payload + " Tj BT (unrelated form words) Tj ET"),
			false,
		},
		{"inside a block in a form", formPDF("BT " + payload + " Tj ET"), true},
	})
}

func TestHostileJavaScriptActionsAreNotExtracted(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	javaScriptStream := func(pdf *syntheticPDF) { pdf.rawStream(8, "", hostileIsolatedContent) }
	requireIsolationCases(t, []isolationCase{
		{"open action literal", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(
				20,
				"<< /Type /Catalog /Pages 21 0 R /OpenAction << /S /JavaScript /JS ("+
					hostileIsolatedContent+") >> >>",
			)
		}), false},
		{"open action stream", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(
				20,
				"<< /Type /Catalog /Pages 21 0 R /OpenAction << /S /JavaScript /JS 8 0 R >> >>",
			)
			javaScriptStream(pdf)
		}), false},
		{
			"page additional action stream",
			isolatedPagePDF("/AA << /O << /S /JavaScript /JS 8 0 R >> >>",
				javaScriptStream),
			false,
		},
		{"document level script name tree", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(20, "<< /Type /Catalog /Pages 21 0 R /Names << /JavaScript 9 0 R >> >>").
				indirectObject(9, "<< /Names [(startup) 10 0 R] >>").
				indirectObject(10, "<< /S /JavaScript /JS 8 0 R >>")
			javaScriptStream(pdf)
		}), false},
		{
			"same stream as page content",
			isolatedPagePDF("/Resources << >> /AA << /O << /S /JavaScript /JS 8 0 R >> >>",
				func(pdf *syntheticPDF) {
					javaScriptStream(pdf)
					pdf.page(11, "/Contents 8 0 R")
				}),
			true,
		},
	})
}

func TestHostileNonContentStreamsAreNotExtractedWhenPagesExist(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	requireIsolationCases(t, []isolationCase{
		{"embedded file", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(20, "<< /Type /Catalog /Names << /EmbeddedFiles << /Names [(note.txt) 9 0 R] >> >> >>").
				indirectObject(9, "<< /Type /Filespec /F (note.txt) /EF << /F 8 0 R >> >>").
				rawStream(8, "/Type /EmbeddedFile", hostileIsolatedContent)
		}), false},
		{"untyped embedded file", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(9, "<< /Type /Filespec /F (note.txt) /EF << /F 8 0 R >> >>").
				rawStream(8, "", hostileIsolatedContent)
		}), false},
		{"metadata", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(20, "<< /Type /Catalog /Metadata 8 0 R >>").
				rawStream(8, "/Type /Metadata /Subtype /XML", hostileIsolatedContent)
		}), false},
		{"xfa form", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(20, "<< /Type /Catalog /AcroForm << /Fields [] /XFA [(template) 8 0 R] >> >>").
				rawStream(8, "", hostileIsolatedContent)
		}), false},
		{
			"font programs",
			isolatedPagePDF("/Resources << /Font << /F1 5 0 R >> >>", func(pdf *syntheticPDF) {
				pdf.indirectObject(5, "<< /Type /Font /Subtype /TrueType /FontDescriptor 6 0 R >>").
					indirectObject(6, "<< /Type /FontDescriptor /FontFile 7 0 R /FontFile2 8 0 R /FontFile3 9 0 R >>").
					rawStream(7, "/Length1 37", hostileIsolatedContent).
					rawStream(8, "", hostileIsolatedContent).
					rawStream(9, "/Subtype /Type1C", hostileIsolatedContent)
			}),
			false,
		},
		{
			"image xobject",
			isolatedPagePDF("/Resources << /XObject << /Im1 8 0 R >> >>", func(pdf *syntheticPDF) {
				pdf.rawStream(
					8,
					"/Type /XObject /Subtype /Image /Width 1 /Height 1",
					hostileIsolatedContent,
				)
			}),
			false,
		},
		{
			"form xobject",
			isolatedPagePDF("/Resources << /XObject << /Im1 8 0 R >> >>", func(pdf *syntheticPDF) {
				pdf.rawStream(
					8,
					"/Type /XObject /Subtype /Form /BBox [0 0 1 1]",
					hostileIsolatedContent,
				)
			}),
			true,
		},
		{"cross reference stream", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.rawStream(8, "/Type /XRef /Size 9 /W [1 2 1]", hostileIsolatedContent)
		}), false},
	})
}

func TestHostilePagelessFallbackSkipsNonContentStreams(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	pagelessPDF := func(objects func(*syntheticPDF)) []byte {
		pdf := newSyntheticPDF().rawStream(2, "", "BT ("+hostileIsolatedPageText+") Tj ET")
		objects(pdf)

		return pdf.finished()
	}
	typedStream := func(dictionary string) []byte {
		return pagelessPDF(
			func(pdf *syntheticPDF) { pdf.rawStream(8, dictionary, hostileIsolatedContent) },
		)
	}
	requireIsolationCases(t, []isolationCase{
		{"untyped stream", typedStream(""), true},
		{"embedded file", typedStream("/Type /EmbeddedFile"), false},
		{"metadata", typedStream("/Type /Metadata /Subtype /XML"), false},
		{"cross reference stream", typedStream("/Type /XRef /Size 9"), false},
		{"image", typedStream("/Subtype /Image /Width 1 /Height 1"), false},
		{"type1 font program", typedStream("/Length1 37 /Length2 0 /Length3 0"), false},
		{"font program by reference", pagelessPDF(func(pdf *syntheticPDF) {
			pdf.indirectObject(6, "<< /Type /FontDescriptor /FontFile2 8 0 R >>").
				rawStream(8, "", hostileIsolatedContent)
		}), false},
		{"tounicode by reference", pagelessPDF(func(pdf *syntheticPDF) {
			pdf.indirectObject(5, "<< /Type /Font /ToUnicode 8 0 R >>").
				rawStream(8, "", hostileIsolatedContent)
		}), false},
		{"metadata by reference", pagelessPDF(func(pdf *syntheticPDF) {
			pdf.indirectObject(20, "<< /Type /Catalog /Metadata 8 0 R >>").
				rawStream(8, "", hostileIsolatedContent)
		}), false},
	})
}

func TestHostileAnnotationsLinksAndOutlinesAreNotExtracted(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	requireIsolationCases(t, []isolationCase{
		{"inline annotations after the page contents", isolatedPagePDF(
			"/Annots [<< /Type /Annot /Subtype /Text /Contents ("+hostileIsolatedPayload+") >> "+
				"<< /Type /Annot /Subtype /Link /A << /S /URI /URI (https://payload.example/"+hostileIsolatedPayload+") >> >>]",
			func(*syntheticPDF) {}), false},
		{
			"indirect annotations",
			isolatedPagePDF("/Annots [9 0 R 10 0 R 11 0 R]", func(pdf *syntheticPDF) {
				pdf.indirectObject(9, "<< /Type /Annot /Subtype /FreeText /Contents ("+hostileIsolatedPayload+") >>").
					indirectObject(10, "<< /Type /Annot /Subtype /Text /Contents 12 0 R >>").
					indirectObject(11, "<< /Type /Annot /Subtype /Widget /FT /Tx /T ("+hostileIsolatedPayload+") /V ("+
						hostileIsolatedPayload+") >>").
					rawStream(12, "", hostileIsolatedContent)
			}),
			false,
		},
		{
			"annotation appearance stream",
			isolatedPagePDF("/Annots [9 0 R]", func(pdf *syntheticPDF) {
				pdf.indirectObject(9, "<< /Type /Annot /Subtype /Stamp /AP << /N 12 0 R >> >>").
					rawStream(12, "/Type /XObject /Subtype /Form /BBox [0 0 1 1]", hostileIsolatedContent)
			}),
			false,
		},
		{"outline items", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(20, "<< /Type /Catalog /Outlines 30 0 R >>").
				indirectObject(30, "<< /Type /Outlines /First 31 0 R /Last 31 0 R /Count 1 >>").
				indirectObject(31, "<< /Title ("+hostileIsolatedPayload+") /Parent 30 0 R /Dest [1 0 R /Fit] >>")
		}), false},
	})
}

func TestHostileInformationTitleComesFromTheTrailerInformationDictionary(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name    string
		objects func(*syntheticPDF)
		want    string
	}{
		{"outline title before the information dictionary", func(pdf *syntheticPDF) {
			pdf.indirectObject(31, "<< /Title (Outline item title) /Parent 30 0 R >>").
				indirectObject(40, "<< /Title (Information title) >>").
				trailer("<< /Info 40 0 R >>")
		}, "Information title"},
		{"information dictionary without a title", func(pdf *syntheticPDF) {
			pdf.indirectObject(31, "<< /Title (Outline item title) /Parent 30 0 R >>").
				indirectObject(40, "<< /Author (Synthetic author) >>").
				trailer("<< /Info 40 0 R >>")
		}, ""},
		{"trailer naming a missing information dictionary", func(pdf *syntheticPDF) {
			pdf.indirectObject(31, "<< /Title (Outline item title) /Parent 30 0 R >>").
				trailer("<< /Info 40 0 R >>")
		}, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, isolatedPagePDF("", testCase.objects))
			requireTextContains(t, extraction.document.Text, hostileIsolatedPageText)
			if extraction.document.Title != testCase.want {
				t.Fatalf("title = %q, want %q", extraction.document.Title, testCase.want)
			}
		})
	}
}

func TestHostileOrphanContentStreamsAreNotExtractedWhenPagesExist(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	requireIsolationCases(t, []isolationCase{
		{"unreferenced stream", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.rawStream(8, "", hostileIsolatedContent)
		}), false},
		{"unreferenced form", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.rawStream(
				8,
				"/Type /XObject /Subtype /Form /BBox [0 0 1 1]",
				hostileIsolatedContent,
			)
		}), false},
		{"stream referenced by a non page dictionary", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.indirectObject(9, "<< /Type /Template /Contents 8 0 R >>").
				rawStream(8, "", hostileIsolatedContent)
		}), false},
		{
			"form named only by an unused resource dictionary",
			isolatedPagePDF("", func(pdf *syntheticPDF) {
				pdf.indirectObject(9, "<< /XObject << /Fm1 8 0 R >> >>").
					rawStream(8, "/Type /XObject /Subtype /Form /BBox [0 0 1 1]", hostileIsolatedContent)
			}),
			false,
		},
		{"stream referenced by the page contents", isolatedPagePDF("", func(pdf *syntheticPDF) {
			pdf.page(11, "/Contents 8 0 R").rawStream(8, "", hostileIsolatedContent)
		}), true},
	})
}

func TestHostileIncrementalUpdatesKeepOnlyTheLatestRevision(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	originalRevision := func() *syntheticPDF {
		return newSyntheticPDF().
			page(1, "/Contents 2 0 R").
			rawStream(2, "", "BT (Original revision words) Tj ET").
			indirectObject(40, "<< /Title (Original title) >>").
			trailer("<< /Info 40 0 R >>").
			text("%%EOF\n")
	}
	for _, testCase := range []struct {
		name    string
		update  func(*syntheticPDF)
		present []string
		absent  []string
		title   string
	}{
		{
			"original revision only", func(*syntheticPDF) {},
			[]string{"Original revision words"},
			nil, "Original title",
		},
		{"update redefines the content stream", func(pdf *syntheticPDF) {
			pdf.rawStream(2, "", "BT (Updated revision words) Tj ET").trailer("<< /Info 40 0 R /Prev 9 >>")
		}, []string{"Updated revision words"}, []string{"Original revision words"}, "Original title"},
		{"update points the page at a new stream", func(pdf *syntheticPDF) {
			pdf.page(1, "/Contents 3 0 R").
				rawStream(3, "", "BT (Updated revision words) Tj ET").
				trailer("<< /Info 40 0 R /Prev 9 >>")
		}, []string{"Updated revision words"}, []string{"Original revision words"}, "Original title"},
		{"update replaces the information dictionary", func(pdf *syntheticPDF) {
			pdf.indirectObject(41, "<< /Title (Updated title) >>").trailer("<< /Info 41 0 R /Prev 9 >>")
		}, []string{"Original revision words"}, nil, "Updated title"},
		{"update redefines the information dictionary in place", func(pdf *syntheticPDF) {
			pdf.indirectObject(40, "<< /Title (Updated title) >>").trailer("<< /Info 40 0 R /Prev 9 >>")
		}, []string{"Original revision words"}, nil, "Updated title"},
		{"update adds a page", func(pdf *syntheticPDF) {
			pdf.page(5, "/Contents 6 0 R").
				rawStream(6, "", "BT (Added page words) Tj ET").
				trailer("<< /Info 40 0 R /Prev 9 >>")
		}, []string{"Original revision words", "Added page words"}, nil, "Original title"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			pdf := originalRevision()
			testCase.update(pdf)
			extraction := requireBoundedExtraction(t, pdf.finished())
			requireTextContains(t, extraction.document.Text, testCase.present...)
			requireTextOmits(t, extraction.document.Text, testCase.absent...)
			if extraction.document.Title != testCase.title {
				t.Fatalf("title = %q, want %q", extraction.document.Title, testCase.title)
			}
		})
	}
}
