package yagopdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

const (
	hostilePlainPageText  = "Plain page words remain visible"
	hostileMemberPageText = "Member page words from the object stream"
	hostileMemberPageDict = "<< /Type /Page /Contents 2 0 R >>"
	hostileNestingDepth   = 1_000_000
)

func pageBesideObjectStream(objectStream func(*syntheticPDF)) []byte {
	pdf := newSyntheticPDF().
		page(10, "/Contents 11 0 R").
		rawStream(11, "", "BT ("+hostilePlainPageText+") Tj ET").
		rawStream(2, "", "BT ("+hostileMemberPageText+") Tj ET")
	objectStream(pdf)

	return pdf.finished()
}

func requireMemberPageUnpacked(t *testing.T, body []byte, unpacked bool) {
	t.Helper()
	extraction := requireBoundedExtraction(t, body)
	requireTextContains(t, extraction.document.Text, hostilePlainPageText)
	if strings.Contains(extraction.document.Text, hostileMemberPageText) != unpacked {
		t.Fatalf("member page unpacked = %t, want %t: %q",
			!unpacked, unpacked, extraction.document.Text[:min(len(extraction.document.Text), 256)])
	}
}

func rawObjectStreamValue(numbers []int, values []string) string {
	var header, members strings.Builder
	for index, number := range numbers {
		fmt.Fprintf(&header, "%d %d ", number, members.Len())
		members.WriteString(values[index] + "\n")
	}
	content := header.String() + members.String()

	return fmt.Sprintf("<< /Type /ObjStm /N %d /First %d /Length %d >>\nstream\n%s\nendstream",
		len(numbers), header.Len(), len(content), content)
}

func flateObjectStreamValue(numbers []int, values []string) string {
	var header, members strings.Builder
	for index, number := range numbers {
		fmt.Fprintf(&header, "%d %d ", number, members.Len())
		members.WriteString(values[index] + "\n")
	}
	encoded := syntheticZlib([]byte(header.String() + members.String()))

	return fmt.Sprintf(
		"<< /Type /ObjStm /Filter /FlateDecode /N %d /First %d /Length %d >>\nstream\n%s\nendstream",
		len(numbers),
		header.Len(),
		len(encoded),
		encoded,
	)
}

func TestHostileObjectStreamCountAndFirstOffsetMustBeUsable(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	memberContent := "1 0 " + hostileMemberPageDict
	cases := []struct {
		name       string
		dictionary string
		content    string
		unpacked   bool
	}{
		{"consistent count and first offset", "/Type /ObjStm /N 1 /First 4", memberContent, true},
		{
			"first offset beyond the decoded data",
			"/Type /ObjStm /N 1 /First 4096",
			memberContent,
			false,
		},
		{"negative first offset", "/Type /ObjStm /N 1 /First -4", memberContent, false},
		{"non-numeric first offset", "/Type /ObjStm /N 1 /First /Four", memberContent, false},
		{
			"thirty digit first offset",
			"/Type /ObjStm /N 1 /First " + strings.Repeat("9", 30),
			memberContent,
			false,
		},
		{
			"member offset beyond the decoded data",
			"/Type /ObjStm /N 1 /First 9",
			"1 4096 " + hostileMemberPageDict,
			false,
		},
		{
			"thirty digit member number", "/Type /ObjStm /N 1 /First 33",
			strings.Repeat("9", 30) + " 0 " + hostileMemberPageDict, false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			requireMemberPageUnpacked(t, pageBesideObjectStream(func(pdf *syntheticPDF) {
				pdf.flateStream(5, testCase.dictionary, testCase.content)
			}), testCase.unpacked)
		})
	}
}

func TestHostileObjectStreamHugeMemberCountsStayBounded(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		count    string
		unpacked bool
	}{
		{"2147483648", true},
		{"9223372036854775807", true},
		{"9223372036854775808", false},
		{strings.Repeat("9", 30), false},
	} {
		t.Run(testCase.count, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			requireMemberPageUnpacked(t, pageBesideObjectStream(func(pdf *syntheticPDF) {
				pdf.flateStream(
					5,
					"/Type /ObjStm /N "+testCase.count+" /First 4",
					"1 0 "+hostileMemberPageDict,
				)
			}), testCase.unpacked)
		})
	}
}

func TestHostileObjectStreamOffsetsThatDescendOrOverlapStayBounded(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	page := hostileMemberPageDict
	filler := "<< /Filler (overlapping member) >>"
	cases := []struct {
		name     string
		content  string
		first    int
		unpacked bool
	}{
		{"ascending offsets", fmt.Sprintf("1 0 3 %d %s\n%s", len(page)+1, page, filler), 0, true},
		{"descending offsets", fmt.Sprintf("3 %d 1 0 %s\n%s", len(page)+1, page, filler), 0, false},
		{"equal offsets leave the earlier member empty", fmt.Sprintf("1 0 3 0 %s", page), 0, true},
		{"offset inside the previous member", fmt.Sprintf("1 0 3 5 %s", page), 0, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			header, _, _ := strings.Cut(testCase.content, "<<")
			requireMemberPageUnpacked(t, pageBesideObjectStream(func(pdf *syntheticPDF) {
				pdf.flateStream(
					5,
					fmt.Sprintf("/Type /ObjStm /N 2 /First %d", len(header)),
					testCase.content,
				)
			}), testCase.unpacked)
		})
	}
}

func TestHostileObjectStreamMemberSetBeyondTheUnpackCapIsNotUnpacked(t *testing.T) {
	skipWhenAllocationIsUnmeasured(t)
	filler := "<< /Filler (" + strings.Repeat("a", 64<<10) + ") >>"
	for _, testCase := range []struct {
		name         string
		decodedBytes int
		unpacked     bool
	}{
		{"member set below the unpack cap", pdfMaxUnpackedObjectStreamBytes - 1<<20, true},
		{"member set beyond the unpack cap", pdfMaxUnpackedObjectStreamBytes + 1<<20, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			fillers := testCase.decodedBytes / len(filler)
			numbers := make([]int, 0, fillers+1)
			values := make([]string, 0, fillers+1)
			for index := range fillers {
				numbers = append(numbers, 100+index)
				values = append(values, filler)
			}
			numbers = append(numbers, 1)
			values = append(values, hostileMemberPageDict)
			requireMemberPageUnpacked(t, pageBesideObjectStream(func(pdf *syntheticPDF) {
				pdf.objectStream(5, numbers, values)
			}), testCase.unpacked)
		})
	}
}

func TestHostileHundredsOfObjectStreamsStayWithinTheObjectCap(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name            string
		membersPerBatch int
		lastUnpacked    bool
	}{
		{"three hundred streams below the object cap", 1, true},
		{"three hundred streams beyond the object cap", 16, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			pdf := newSyntheticPDF().
				rawStream(2, "", "BT (First object stream page words) Tj ET").
				rawStream(4, "", "BT (Last object stream page words) Tj ET")
			for batch := range 300 {
				numbers := make([]int, 0, testCase.membersPerBatch)
				values := make([]string, 0, testCase.membersPerBatch)
				for member := range testCase.membersPerBatch {
					numbers = append(numbers, 1000+batch*testCase.membersPerBatch+member)
					values = append(values, "<< /Filler true >>")
				}
				switch batch {
				case 0:
					numbers[0], values[0] = 1, "<< /Type /Page /Contents 2 0 R >>"
				case 299:
					numbers[0], values[0] = 3, "<< /Type /Page /Contents 4 0 R >>"
				}
				pdf.objectStream(10000+batch, numbers, values)
			}
			extraction := requireBoundedExtraction(t, pdf.finished())
			requireTextContains(t, extraction.document.Text, "First object stream page words")
			if got := strings.Contains(
				extraction.document.Text,
				"Last object stream page words",
			); got != testCase.lastUnpacked {
				t.Fatalf(
					"last object stream page unpacked = %t, want %t",
					got,
					testCase.lastUnpacked,
				)
			}
		})
	}
}

func TestHostileNestedObjectStreamMembersAreNotUnpackedRecursively(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	inner := rawObjectStreamValue([]int{1}, []string{hostileMemberPageDict})
	t.Run("object stream at the top level", func(t *testing.T) {
		parallelWhenAllocationIsUnmeasured(t)
		requireMemberPageUnpacked(t, pageBesideObjectStream(func(pdf *syntheticPDF) {
			pdf.indirectObject(7, inner)
		}), true)
	})
	t.Run("object stream inside an object stream", func(t *testing.T) {
		parallelWhenAllocationIsUnmeasured(t)
		requireMemberPageUnpacked(t, pageBesideObjectStream(func(pdf *syntheticPDF) {
			pdf.objectStream(5, []int{7}, []string{inner})
		}), false)
	})
	t.Run("two hundred nested object streams", func(t *testing.T) {
		parallelWhenAllocationIsUnmeasured(t)
		nested := flateObjectStreamValue([]int{1}, []string{hostileMemberPageDict})
		for level := range 200 {
			nested = flateObjectStreamValue([]int{20 + level}, []string{nested})
		}
		requireMemberPageUnpacked(t, pageBesideObjectStream(func(pdf *syntheticPDF) {
			pdf.indirectObject(5, nested)
		}), false)
	})
}

func TestHostileObjectStreamRedefinitionsKeepTheLastDefinitionInFileOrder(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	plainPage := func(pdf *syntheticPDF) { pdf.page(1, "/Contents 2 0 R") }
	memberPage := func(pdf *syntheticPDF) {
		pdf.objectStream(5, []int{1}, []string{"<< /Type /Page /Contents 3 0 R >>"})
	}
	for _, testCase := range []struct {
		name    string
		earlier func(*syntheticPDF)
		later   func(*syntheticPDF)
		kept    string
		dropped string
	}{
		{
			"object stream member after the plain page", plainPage, memberPage,
			"Member revision words", "Plain revision words",
		},
		{
			"plain page after the object stream member", memberPage, plainPage,
			"Plain revision words", "Member revision words",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			pdf := newSyntheticPDF().
				rawStream(2, "", "BT (Plain revision words) Tj ET").
				rawStream(3, "", "BT (Member revision words) Tj ET")
			testCase.earlier(pdf)
			testCase.later(pdf)
			extraction := requireBoundedExtraction(t, pdf.finished())
			requireTextContains(t, extraction.document.Text, testCase.kept)
			requireTextOmits(t, extraction.document.Text, testCase.dropped)
		})
	}
}

func TestHostileFontsBeyondTheFontTableCapAreNotDecoded(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	fonts := pdfMaxFontTables + 36
	var resources, content strings.Builder
	pdf := newSyntheticPDF()
	for font := 1; font <= fonts; font++ {
		fmt.Fprintf(&resources, "/F%d %d 0 R ", font, 100+font)
		pdf.indirectObject(100+font,
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	}
	for font, words := range map[int]string{
		1:                    "first font words",
		pdfMaxFontTables:     "last admitted font words",
		pdfMaxFontTables + 1: "font beyond the cap words",
		fonts:                "final font words",
	} {
		fmt.Fprintf(&content, "BT /F%d 12 Tf (%s) Tj ET\n", font, words)
	}
	body := pdf.page(1, "/Resources << /Font << "+resources.String()+">> >> /Contents 2 0 R").
		rawStream(2, "", content.String()).
		finished()
	extraction := requireBoundedExtraction(t, body)
	requireTextContains(t, extraction.document.Text, "first font words", "last admitted font words")
	requireTextOmits(t, extraction.document.Text, "font beyond the cap words", "final font words")
}

func TestHostileObjectsBeyondTheIndirectObjectCapAreNotRead(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	pdf := newSyntheticPDF().
		page(1, "/Contents 2 0 R").
		rawStream(2, "", "BT (Page within the object cap) Tj ET")
	for object := range pdfMaxIndirectObjects + 1000 {
		pdf.indirectObject(100+object, "<< /Filler true >>")
	}
	body := pdf.page(9000, "/Contents 9001 0 R").
		rawStream(9001, "", "BT (Page beyond the object cap) Tj ET").
		finished()
	extraction := requireBoundedExtraction(t, body)
	requireTextContains(t, extraction.document.Text, "Page within the object cap")
	requireTextOmits(t, extraction.document.Text, "Page beyond the object cap")
}

func TestHostileRepeatedContentReferencesDoNotAmplifyText(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	repeated := make([]int, 10_000)
	for index := range repeated {
		repeated[index] = 2
	}
	pagesSharingOneStream := newSyntheticPDF()
	for page := range 4000 {
		pagesSharingOneStream.page(100+page, "/Contents 2 0 R")
	}
	for _, testCase := range []struct {
		name string
		body []byte
	}{
		{"one stream once", singlePagePDF("BT (Repeated stream words) Tj ET")},
		{"contents array naming one stream ten thousand times", newSyntheticPDF().
			page(1, "/Contents "+contentsArray(repeated...)).
			rawStream(2, "", "BT (Repeated stream words) Tj ET").
			finished()},
		{"four thousand pages sharing one stream", pagesSharingOneStream.
			rawStream(2, "", "BT (Repeated stream words) Tj ET").
			finished()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, testCase.body)
			if got := strings.Count(
				extraction.document.Text,
				"Repeated stream words",
			); got < 1 ||
				got > pdfMaxStreams {
				t.Fatalf("repeated stream text occurs %d times", got)
			}
		})
	}
}

func TestHostileFormCyclesVisitEachFormOnce(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name  string
		forms func(*syntheticPDF)
	}{
		{"self-referencing form", func(pdf *syntheticPDF) {
			pdf.rawStream(3, "/Type /XObject /Subtype /Form /Resources << /XObject << /Self 3 0 R >> >>",
				"BT (Cyclic form alpha words) Tj ET")
		}},
		{"two forms referencing each other", func(pdf *syntheticPDF) {
			pdf.rawStream(3, "/Type /XObject /Subtype /Form /Resources << /XObject << /B 4 0 R >> >>",
				"BT (Cyclic form alpha words) Tj ET").
				rawStream(4, "/Type /XObject /Subtype /Form /Resources << /XObject << /A 3 0 R >> >>",
					"BT (Cyclic form beta words) Tj ET")
		}},
		{"form resources referencing their own dictionary", func(pdf *syntheticPDF) {
			pdf.rawStream(3, "/Type /XObject /Subtype /Form /Resources 6 0 R",
				"BT (Cyclic form alpha words) Tj ET").
				indirectObject(6, "<< /XObject << /A 3 0 R /Again 6 0 R >> /Parent 6 0 R >>")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			pdf := newSyntheticPDF().
				page(1, "/Resources << /XObject << /A 3 0 R >> >> /Contents 2 0 R").
				rawStream(2, "", "BT (Page words around the form) Tj ET")
			testCase.forms(pdf)
			extraction := requireBoundedExtraction(t, pdf.finished())
			requireTextContains(t, extraction.document.Text, "Page words around the form")
			if got := strings.Count(extraction.document.Text, "Cyclic form alpha words"); got != 1 {
				t.Fatalf("cyclic form text occurs %d times: %q", got, extraction.document.Text)
			}
		})
	}
}

func TestHostileFormChainsStopAtTheStreamCap(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	pdf := newSyntheticPDF().
		page(1, "/Resources << /XObject << /X 100 0 R >> >> /Contents 2 0 R").
		rawStream(2, "", "BT (Page words before a form chain) Tj ET")
	forms := pdfMaxIndirectObjects
	for form := range forms {
		word := "chained"
		switch form {
		case 0:
			word = "Firstchainedform"
		case forms - 1:
			word = "Finalchainedform"
		}
		pdf.rawStream(100+form,
			fmt.Sprintf("/Subtype /Form /Resources << /XObject << /X %d 0 R >> >>", 101+form),
			fmt.Sprintf("BT (%s) Tj ET", word))
	}
	extraction := requireBoundedExtraction(t, pdf.finished())
	requireTextContains(
		t,
		extraction.document.Text,
		"Page words before a form chain",
		"Firstchainedform",
	)
	requireTextOmits(t, extraction.document.Text, "Finalchainedform")
	if got := strings.Count(extraction.document.Text, "chained"); got > pdfMaxStreams {
		t.Fatalf("form chain text occurs %d times, above the %d stream cap", got, pdfMaxStreams)
	}
}

func TestHostilePageTreeCyclesTerminate(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name  string
		page  string
		nodes func(*syntheticPDF)
	}{
		{"page naming itself as parent", "/Parent 1 0 R /Contents 2 0 R", func(*syntheticPDF) {}},
		{"two parents naming each other", "/Parent 10 0 R /Contents 2 0 R", func(pdf *syntheticPDF) {
			pdf.indirectObject(10, "<< /Type /Pages /Parent 11 0 R /Kids [1 0 R] /Count 1 >>").
				indirectObject(11, "<< /Type /Pages /Parent 10 0 R /Kids [10 0 R] /Count 1 >>")
		}},
		{"pages node listing itself as a kid", "/Parent 10 0 R /Contents 2 0 R", func(pdf *syntheticPDF) {
			pdf.indirectObject(10, "<< /Type /Pages /Kids [10 0 R 1 0 R] /Count 2 >>")
		}},
		{"kids cycle through two nodes", "/Parent 10 0 R /Contents 2 0 R", func(pdf *syntheticPDF) {
			pdf.indirectObject(10, "<< /Type /Pages /Kids [11 0 R 1 0 R] /Count 2 >>").
				indirectObject(11, "<< /Type /Pages /Parent 10 0 R /Kids [10 0 R] /Count 1 >>")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			pdf := newSyntheticPDF().
				page(1, testCase.page).
				rawStream(2, "", "BT (Page tree cycle words) Tj ET")
			testCase.nodes(pdf)
			extraction := requireBoundedExtraction(t, pdf.finished())
			if extraction.document.Text != "Page tree cycle words" {
				t.Fatalf("page tree cycle text = %q", extraction.document.Text)
			}
		})
	}
}

func TestHostileInheritedResourcesStillReachFormsThroughTheParentChain(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name      string
		resources string
		reached   bool
	}{
		{"resources on the grandparent", "/Resources << /XObject << /X 3 0 R >> >>", true},
		{"no resources anywhere in the chain", "", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			body := newSyntheticPDF().
				page(1, "/Parent 10 0 R /Contents 2 0 R").
				rawStream(2, "", "BT (Page words beside an inherited form) Tj ET").
				rawStream(3, "/Subtype /Form", "BT (Inherited form words) Tj ET").
				indirectObject(10, "<< /Type /Pages /Parent 11 0 R /Kids [1 0 R] >>").
				indirectObject(11, "<< /Type /Pages "+testCase.resources+" /Kids [10 0 R] >>").
				finished()
			extraction := requireBoundedExtraction(t, body)
			requireTextContains(t, extraction.document.Text, "Page words beside an inherited form")
			if got := strings.Contains(
				extraction.document.Text,
				"Inherited form words",
			); got != testCase.reached {
				t.Fatalf("inherited form reached = %t, want %t", got, testCase.reached)
			}
		})
	}
}

func TestHostileSelfReferencingContentArraysTerminate(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	longChain := newSyntheticPDF().page(1, "/Contents 100 0 R")
	for link := range 2000 {
		longChain.indirectObject(100+link, fmt.Sprintf("[%d 0 R]", 101+link))
	}
	longChain.indirectObject(2100, "[2 0 R 100 0 R]")
	for _, testCase := range []struct {
		name string
		pdf  *syntheticPDF
	}{
		{"array naming itself", newSyntheticPDF().
			page(1, "/Contents 5 0 R").
			indirectObject(5, "[5 0 R 2 0 R 5 0 R]")},
		{"two arrays naming each other", newSyntheticPDF().
			page(1, "/Contents 5 0 R").
			indirectObject(5, "[6 0 R]").
			indirectObject(6, "[5 0 R 2 0 R]")},
		{"two thousand linked arrays ending in a cycle", longChain},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			body := testCase.pdf.rawStream(2, "", "BT (Array cycle words) Tj ET").finished()
			extraction := requireBoundedExtraction(t, body)
			if extraction.document.Text != "Array cycle words" {
				t.Fatalf("array cycle text = %q", extraction.document.Text)
			}
		})
	}
}

func TestHostileDeepNestingStaysBounded(t *testing.T) {
	skipWhenAllocationIsUnmeasured(t)
	for _, opener := range []string{"[", "(", "<<", "<"} {
		deep := strings.Repeat(opener, hostileNestingDepth)
		for _, testCase := range []struct {
			name string
			body []byte
		}{
			{"inside a text block", singlePagePDF(
				"BT (Legitimate words before deep nesting) Tj ET\nBT " + deep + " ET")},
			{"outside a text block", singlePagePDF(
				"BT (Legitimate words before deep nesting) Tj ET\n" + deep)},
			{"inside a later page dictionary", newSyntheticPDF().
				page(1, "/Contents 2 0 R").
				rawStream(2, "", "BT (Legitimate words before deep nesting) Tj ET").
				page(3, "/Deep "+deep).
				finished()},
			{"inside a later page resources", newSyntheticPDF().
				page(1, "/Contents 2 0 R").
				rawStream(2, "", "BT (Legitimate words before deep nesting) Tj ET").
				page(3, "/Resources << /XObject "+deep+" >>").
				finished()},
			{"inside the information title", newSyntheticPDF().
				page(1, "/Contents 2 0 R").
				rawStream(2, "", "BT (Legitimate words before deep nesting) Tj ET").
				indirectObject(3, "<< /Title "+deep+" >>").
				trailer("<< /Info 3 0 R >>").
				finished()},
		} {
			t.Run(fmt.Sprintf("%q %s", opener, testCase.name), func(t *testing.T) {
				parallelWhenAllocationIsUnmeasured(t)
				extraction := requireBoundedExtraction(t, testCase.body)
				if !extraction.readable ||
					!strings.HasPrefix(
						extraction.document.Text,
						"Legitimate words before deep nesting",
					) {
					t.Fatalf("deep nesting extraction = %t %q", extraction.readable,
						extraction.document.Text[:min(len(extraction.document.Text), 128)])
				}
			})
		}
	}
}

func TestHostileHugeTokensStayBounded(t *testing.T) {
	skipWhenAllocationIsUnmeasured(t)
	hugeName := "/" + strings.Repeat("N", 1<<20)
	hugeNumber := strings.Repeat("9", 100_000)
	escapedLiteral := strings.Repeat(`\\\(\)\n\101\0`, (10<<20)/14)
	for _, testCase := range []struct {
		name string
		body []byte
	}{
		{"one mebibyte name as a font selection", singlePagePDF(
			"BT " + hugeName + " 12 Tf (Legitimate words after the huge token) Tj ET")},
		{"one mebibyte name as a resource key", newSyntheticPDF().
			page(1, "/Resources << /Font << "+hugeName+" 3 0 R >> >> /Contents 2 0 R").
			indirectObject(3, "<< /Type /Font /Subtype /Type1 /Encoding /WinAnsiEncoding >>").
			rawStream(2, "", "BT (Legitimate words after the huge token) Tj ET").
			finished()},
		{"one mebibyte name as a page dictionary key", newSyntheticPDF().
			page(1, hugeName+" true /Contents 2 0 R").
			rawStream(2, "", "BT (Legitimate words after the huge token) Tj ET").
			finished()},
		{"hundred thousand digit kerning number", singlePagePDF(
			"BT [(Legitimate words after) -" + hugeNumber + " (the huge token)] TJ ET")},
		{"hundred thousand digit font size", newSyntheticPDF().
			page(1, "/Resources << /Font << /F1 3 0 R >> >> /Contents 2 0 R").
			indirectObject(3, "<< /Type /Font /Subtype /Type1 /Encoding /WinAnsiEncoding >>").
			rawStream(2, "", "BT /F1 "+hugeNumber+" Tf (Legitimate words after the huge token) Tj ET").
			finished()},
		{"hundred thousand digit object number", newSyntheticPDF().
			page(1, "/Contents 2 0 R /Extra "+hugeNumber+" 0 R").
			rawStream(2, "", "BT (Legitimate words after the huge token) Tj ET").
			indirectObject(0, hugeNumber+" 0 obj << /Filler true >>").
			finished()},
		{"hundred thousand digit stream length", newSyntheticPDF().
			page(1, "/Contents 2 0 R").
			text("2 0 obj\n<< /Length " + hugeNumber + " >>\nstream\nBT (Legitimate words after the huge token) Tj ET\nendstream\nendobj\n").
			finished()},
		{"ten mebibyte escaped literal", newSyntheticPDF().
			page(1, "/Contents [2 0 R 3 0 R]").
			rawStream(2, "", "BT (Legitimate words after the huge token) Tj ET").
			flateStream(3, "", "BT ("+escapedLiteral+") Tj ET").
			finished()},
		{"unterminated literal", singlePagePDF(
			"BT (Legitimate words after the huge token) Tj ET\nBT (unterminated literal " +
				strings.Repeat("words ", 100_000))},
		{"unterminated hex string", singlePagePDF(
			"BT (Legitimate words after the huge token) Tj ET\nBT <" + strings.Repeat("41", 500_000) + " ET")},
		{"unterminated content dictionary", singlePagePDF(
			"BT (Legitimate words after the huge token) Tj ET\nBT << /Unterminated (hidden words) Tj ET")},
		{"unterminated later page dictionary", newSyntheticPDF().
			page(1, "/Contents 2 0 R").
			rawStream(2, "", "BT (Legitimate words after an earlier valid object) Tj ET").
			indirectObject(3, "<< /Type /Page /Contents 4 0 R").
			finished()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, testCase.body)
			if !extraction.readable {
				t.Fatal("legitimate text beside the huge token is unreadable")
			}
			requireTextContains(t, strings.Join(strings.Fields(extraction.document.Text), " "),
				"Legitimate words after")
		})
	}
}

func TestHostileScannerPathologiesStayBounded(t *testing.T) {
	skipWhenAllocationIsUnmeasured(t)
	legitimatePage := newSyntheticPDF().
		page(1, "/Contents 2 0 R").
		rawStream(2, "", "BT (Legitimate words before the pathology) Tj ET")
	withTail := func(tail string) []byte {
		return append(bytes.Clone(legitimatePage.body.Bytes()), tail...)
	}
	for _, testCase := range []struct {
		name string
		body []byte
	}{
		{"object headers without end markers", withTail(strings.Repeat("1 0 obj\n", (4<<20)/8))},
		{"object headers before the legitimate page", append(
			[]byte("%PDF-1.7\n"+strings.Repeat("7 0 obj\n", (4<<20)/8)),
			legitimatePage.body.Bytes()[len("%PDF-1.7\n"):]...)},
		{"stream without an end marker", withTail("3 0 obj\n<< /Length 9 >>\nstream\n" +
			strings.Repeat("unended stream data ", 100_000) + "\nendobj\n")},
		{"stream keywords without data", withTail(strings.Repeat("stream\n", 200_000))},
		{"thousands of title tokens", withTail("3 0 obj\n<< " + strings.Repeat("/Title ", 200_000) + ">>\nendobj\n")},
		{"thousands of information tokens", withTail(strings.Repeat("/Info /Info 1 0 ", 100_000))},
		{"thousands of near-miss encryption names", withTail("trailer\n<< " +
			strings.Repeat("/EncryptMetadata false ", 100_000) + ">>\n")},
		{"thousands of end markers", withTail(strings.Repeat("endobj\nendstream\n", 200_000))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, testCase.body)
			requireTextContains(
				t,
				extraction.document.Text,
				"Legitimate words before the pathology",
			)
		})
	}
}

func TestHostileRepeatedEncryptionReferencesDecideLikeASingleReference(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	legitimatePage := newSyntheticPDF().
		page(1, "/Contents 2 0 R").
		rawStream(2, "", "BT (Legitimate words before the pathology) Tj ET")
	withTrailer := func(references int) []byte {
		return append(bytes.Clone(legitimatePage.body.Bytes()),
			"trailer\n<< "+strings.Repeat("/Encrypt 9 0 R ", references)+">>\n"...)
	}
	single := requireBoundedExtraction(t, withTrailer(1))
	repeated := requireBoundedExtraction(t, withTrailer(100_000))
	if repeated.readable != single.readable || repeated.document != single.document {
		t.Fatalf(
			"repeated references decide readable %v text %q, a single reference decides readable %v text %q",
			repeated.readable,
			repeated.document.Text,
			single.readable,
			single.document.Text,
		)
	}
}

func TestHostileLineEndingAndNulVariantsDropNulInsideStringsAndRefuseNulSeparatedSyntax(
	t *testing.T,
) {
	parallelWhenAllocationIsUnmeasured(t)
	base := string(singlePagePDF("BT (Line ending variant words) Tj ET"))
	interleaved := make([]byte, 0, 2*len(base))
	for _, value := range []byte(base) {
		interleaved = append(interleaved, value, 0)
	}
	for _, testCase := range []struct {
		name string
		body []byte
		want string
	}{
		{"line feed only", []byte(base), "Line ending variant words"},
		{"carriage return only", []byte(strings.ReplaceAll(base, "\n", "\r")), "Line ending variant words"},
		{"carriage return and line feed", []byte(strings.ReplaceAll(base, "\n", "\r\n")), "Line ending variant words"},
		{"nul run inside the content stream", singlePagePDF(
			"BT (Line ending variant words) Tj ET\n" + strings.Repeat("\x00", 1<<20)), "Line ending variant words"},
		{
			"nul inside the shown string", singlePagePDF("BT (Line ending\x00 variant\x00\x00 words) Tj ET"),
			"Line ending variant words",
		},
		{"nul in place of every space", []byte(strings.ReplaceAll(base, " ", "\x00")), "Lineendingvariantwords"},
		{"nul after every byte", interleaved, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, testCase.body)
			if extraction.readable != (testCase.want != "") ||
				extraction.document.Text != testCase.want {
				t.Fatalf(
					"variant extraction = %t %q, want %q",
					extraction.readable,
					extraction.document.Text,
					testCase.want,
				)
			}
		})
	}
}

func TestHostileScannerAllocationGrowsLinearlyWithInputSize(t *testing.T) {
	skipWhenAllocationIsUnmeasured(t)
	shapes := map[string]func(size int) []byte{
		"object headers without end markers": func(size int) []byte {
			return append([]byte("%PDF-1.7\n"), strings.Repeat("1 0 obj\n", size/8)...)
		},
		"small objects": func(size int) []byte {
			return append(
				[]byte("%PDF-1.7\n"),
				strings.Repeat("1 0 obj\n<< /Filler true >>\nendobj\n", size/34)...)
		},
		"information tokens": func(size int) []byte {
			return append([]byte("%PDF-1.7\n"), strings.Repeat("/Info /Title (x) ", size/17)...)
		},
		"text operators": func(size int) []byte {
			return singlePagePDF("BT " + strings.Repeat("(filler words) Tj ", size/18) + "ET")
		},
		"loose streams": func(size int) []byte {
			return append([]byte("%PDF-1.7\n"),
				strings.Repeat("<< >> stream\nBT (loose words) Tj ET\nendstream\n", size/48)...)
		},
	}
	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			small := requireBoundedExtraction(t, shape(1<<20)).allocatedBytes
			large := requireBoundedExtraction(t, shape(4<<20)).allocatedBytes
			if allocationMeasured && large > 8*max(small, 1<<20) {
				t.Fatalf(
					"allocated %d bytes for 1 MiB and %d bytes for 4 MiB, ratio above 8",
					small,
					large,
				)
			}
		})
	}
}

func TestHostilePolyglotBodiesAreRefusedByTheWrongReader(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	pdf := singlePagePDF("BT (Polyglot portable document words) Tj ET")
	postScript := []byte("%!PS-Adobe-3.0\n/Helvetica findfont 12 scalefont setfont\n" +
		"72 720 moveto (Polyglot postscript program words) show\nshowpage\n")
	noise := syntheticBinaryNoise(t, 1<<20)
	for _, testCase := range []struct {
		name       string
		body       []byte
		pdf        bool
		postScript bool
	}{
		{"portable document", pdf, true, false},
		{"postscript program", postScript, false, true},
		{"portable document with a trailing archive", append(bytes.Clone(pdf), "PK\x03\x04archive"...), true, false},
		{"markup before the portable document", append([]byte("<html><body>"), pdf...), false, false},
		{"archive before the portable document", append([]byte("PK\x03\x04\x14\x00\x00\x00"), pdf...), false, false},
		{"postscript header before the portable document", append([]byte("%!PS-Adobe-3.0\n"), pdf...), false, false},
		{"leading space before the portable document", append([]byte(" "), pdf...), false, false},
		{"header only", []byte("%PDF-1.7"), false, false},
		{"bare signature", []byte("%PDF"), false, false},
		{"empty body", nil, false, false},
		{"portable document signature over random bytes", append([]byte("%PDF-1.7\n"), noise...), false, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, testCase.body)
			if extraction.readable != testCase.pdf {
				t.Fatalf(
					"ReadPDF readable = %t, want %t: %q",
					extraction.readable,
					testCase.pdf,
					extraction.document.Text,
				)
			}
			document, readable := ReadPostScript(testCase.body)
			requireExtractionInvariants(t, document, readable)
			if readable != testCase.postScript {
				t.Fatalf(
					"ReadPostScript readable = %t, want %t: %q",
					readable,
					testCase.postScript,
					document.Text,
				)
			}
		})
	}
}
