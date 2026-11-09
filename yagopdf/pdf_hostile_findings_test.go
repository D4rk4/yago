package yagopdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	hostileFindingInputBytes = 1 << 20
	hostileFindingTimeBound  = 2 * time.Second * raceDetectorSlowdown
)

type timedExtraction struct {
	document Document
	readable bool
	elapsed  time.Duration
	finished bool
}

func readPDFWithinTimeBound(body []byte, bound time.Duration) timedExtraction {
	started := time.Now()
	completed := make(chan timedExtraction, 1)
	go func() {
		document, readable := ReadPDF(body)
		completed <- timedExtraction{
			document: document,
			readable: readable,
			elapsed:  time.Since(started),
			finished: true,
		}
	}()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case extraction := <-completed:
		return extraction
	case <-timer.C:
		return timedExtraction{elapsed: bound}
	}
}

func requireReadPDFWithinTimeBound(t *testing.T, body []byte, bound time.Duration) {
	t.Helper()
	extraction := readPDFWithinTimeBound(body, bound)
	if !extraction.finished {
		t.Fatalf("ReadPDF on %d input bytes did not return within %s", len(body), bound)
	}
	requireExtractionInvariants(t, extraction.document, extraction.readable)
	t.Logf("input bytes %d, elapsed %s", len(body), extraction.elapsed)
}

func requireHostileShapeWithinTheTimeBound(t *testing.T, control []byte, hostile []byte) {
	t.Helper()
	t.Run("same-size control shape", func(t *testing.T) {
		requireReadPDFWithinTimeBound(t, control, hostileFindingTimeBound)
	})
	t.Run("hostile shape", func(t *testing.T) {
		requireReadPDFWithinTimeBound(t, hostile, hostileFindingTimeBound)
	})
}

func sharedPageResourcesShape(size int, fillerInSharedResources bool) []byte {
	var body bytes.Buffer
	body.WriteString("%PDF-1.4\n")
	for page := 2; page < 4094; page++ {
		fmt.Fprintf(&body, "%d 0 obj<</Type/Page/Resources 1 0 R>>endobj\n", page)
	}
	filler := strings.Repeat("a", size-body.Len()-64)
	if fillerInSharedResources {
		body.WriteString("1 0 obj<<" + filler + "/XObject<</X 9 0 R>>>>endobj\n")
	} else {
		body.WriteString("1 0 obj<</XObject<</X 9 0 R>>>>endobj\n")
		body.WriteString("4094 0 obj<<" + filler + ">>endobj\n")
	}
	body.WriteString("%%EOF")

	return body.Bytes()
}

func repeatedFontReferenceShape(size int, fillerInReferencedObject bool) []byte {
	var body bytes.Buffer
	body.WriteString("%PDF-1.4\n")
	body.WriteString("2 0 obj<<" + strings.Repeat("/F 1 0 R ", 4096) + ">>endobj\n")
	filler := strings.Repeat("a", size-body.Len()-32)
	if fillerInReferencedObject {
		body.WriteString("1 0 obj<<" + filler + ">>endobj\n")
	} else {
		body.WriteString("1 0 obj<<>>endobj\n")
		body.WriteString("3 0 obj<<" + filler + ">>endobj\n")
	}
	body.WriteString("%%EOF")

	return body.Bytes()
}

func parentChainShape(size int, chained bool) []byte {
	var body bytes.Buffer
	body.WriteString("%PDF-1.4\n")
	for page := range 2046 {
		fmt.Fprintf(&body, "%d 0 obj<</Type/Page/Parent 20000 0 R>>endobj\n", 10000+page)
	}
	filler := strings.Repeat("a", (size-body.Len())/2046-64)
	for index := range 2046 {
		parent := 20001 + index
		if !chained {
			parent = 30000
		}
		fmt.Fprintf(
			&body,
			"%d 0 obj<</Type/Pages %s/Parent %d 0 R>>endobj\n",
			20000+index,
			filler,
			parent,
		)
	}
	body.WriteString("%%EOF")

	return body.Bytes()
}

func TestHostileSharedPageResourcesAreResolvedOnce(t *testing.T) {
	t.Parallel()
	requireHostileShapeWithinTheTimeBound(t,
		sharedPageResourcesShape(hostileFindingInputBytes, false),
		sharedPageResourcesShape(hostileFindingInputBytes, true))
}

func TestHostileRepeatedFontReferencesAreCheckedOnce(t *testing.T) {
	t.Parallel()
	requireHostileShapeWithinTheTimeBound(t,
		repeatedFontReferenceShape(hostileFindingInputBytes, false),
		repeatedFontReferenceShape(hostileFindingInputBytes, true))
}

func TestHostileParentChainsAreWalkedOnceWithinTheDepthLimit(t *testing.T) {
	t.Parallel()
	requireHostileShapeWithinTheTimeBound(t,
		parentChainShape(hostileFindingInputBytes, false),
		parentChainShape(hostileFindingInputBytes, true))
}

func TestHostileEndTextInsideATokenKeepsTheTextBlockOpen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			"uppercase word in the only shown string",
			"BT (MARKET REPORT legitimate words) Tj ET",
			"MARKET REPORT legitimate words",
		},
		{
			"uppercase word in a later shown string",
			"BT (legitimate words first) Tj (MARKET second words) Tj ET",
			"legitimate words first MARKET second words",
		},
		{
			"name operand carrying the keyword",
			"BT /RESET gs (legitimate visible words) Tj ET",
			"legitimate visible words",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document, readable := ReadPDF(singlePagePDF(testCase.content))
			requireExtractionInvariants(t, document, readable)
			if !readable || document.Text != testCase.want {
				t.Fatalf("extraction = %t %q, want %q", readable, document.Text, testCase.want)
			}
		})
	}
}

func TestHostileBeginTextInsideATokenOpensNoTextBlock(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{
			"name operand carrying the keyword",
			"/OBTAIN gs (payload outside block) Tj ET BT (legitimate visible words) Tj ET",
		},
		{
			"shown string carrying the keyword",
			"(SUBTOTAL) Tj (payload outside block) Tj BT (legitimate visible words) Tj ET",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document, readable := ReadPDF(singlePagePDF(testCase.content))
			requireExtractionInvariants(t, document, readable)
			requireTextContains(t, document.Text, "legitimate visible words")
			requireTextOmits(t, document.Text, "payload outside block")
		})
	}
}

func TestHostilePostScriptRandomBinaryYieldsNoText(t *testing.T) {
	t.Parallel()
	noise := syntheticBinaryNoise(t, 4096)
	t.Run("postscript program", func(t *testing.T) {
		document, readable := ReadPostScript(
			[]byte("%!PS\n72 720 moveto (Legitimate postscript words) show\n"),
		)
		requireExtractionInvariants(t, document, readable)
		if !readable || document.Text != "Legitimate postscript words" {
			t.Fatalf("postscript extraction = %t %q", readable, document.Text)
		}
	})
	t.Run("postscript signature over random bytes", func(t *testing.T) {
		document, readable := ReadPostScript(append([]byte("%!PS\n"), noise...))
		requireExtractionInvariants(t, document, readable)
		if readable {
			t.Fatalf("random binary indexed as %d text bytes: %q", len(document.Text),
				document.Text[:min(len(document.Text), 64)])
		}
	})
}

func TestHostileEndstreamInsideStreamDataInjectsNoObjects(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name       string
		attachment string
	}{
		{"attachment without stream keywords", "attached plain bytes\n"},
		{"attachment carrying an inner object", "attached bytes endstream\nendobj\n2 0 obj\n<< >>\nstream\n" +
			"BT (Attachment payload words) Tj ET\nendstream\nendobj\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := newSyntheticPDF().
				page(1, "/Contents 2 0 R").
				rawStream(2, "", "BT (Legitimate page words here) Tj ET").
				rawStream(7, "/Type /EmbeddedFile", testCase.attachment).
				finished()
			document, readable := ReadPDF(body)
			requireExtractionInvariants(t, document, readable)
			requireTextContains(t, document.Text, "Legitimate page words here")
			requireTextOmits(t, document.Text, "Attachment payload words")
		})
	}
}

func TestHostilePageRemovedByAnIncrementalUpdateIsNotRead(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		update string
	}{
		{"original page tree", ""},
		{"update removes the second page", "10 0 obj\n<< /Type /Pages /Kids [1 0 R] /Count 1 >>\nendobj\n" +
			"trailer\n<< /Root 20 0 R /Prev 9 >>\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := newSyntheticPDF().
				indirectObject(20, "<< /Type /Catalog /Pages 10 0 R >>").
				indirectObject(10, "<< /Type /Pages /Kids [1 0 R 3 0 R] /Count 2 >>").
				page(1, "/Parent 10 0 R /Contents 2 0 R").
				rawStream(2, "", "BT (Legitimate page words here) Tj ET").
				page(3, "/Parent 10 0 R /Contents 4 0 R").
				rawStream(4, "", "BT (Removed page words) Tj ET").
				trailer("<< /Root 20 0 R >>").
				text("%%EOF\n").
				text(testCase.update).
				finished()
			document, readable := ReadPDF(body)
			requireExtractionInvariants(t, document, readable)
			requireTextContains(t, document.Text, "Legitimate page words here")
			removed := testCase.update != ""
			if extracted := strings.Contains(
				document.Text,
				"Removed page words",
			); extracted == removed {
				t.Fatalf(
					"page removed %t, removed page text extracted %t: %q",
					removed,
					extracted,
					document.Text,
				)
			}
		})
	}
}

func TestHostileOutlineTitleNeverBecomesTheDocumentTitle(
	t *testing.T,
) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		trailer string
		want    string
	}{
		{"trailer information reference", "trailer\n<< /Info 40 0 R >>\n", "Information title"},
		{"no trailer information reference", "", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			pdf := newSyntheticPDF().
				indirectObject(31, "<< /Title (Outline item title) /Parent 30 0 R /Dest [1 0 R /Fit] >>").
				page(1, "/Contents 2 0 R").
				rawStream(2, "", "BT (Legitimate page words here) Tj ET")
			if testCase.trailer != "" {
				pdf.indirectObject(40, "<< /Title (Information title) >>").text(testCase.trailer)
			}
			document, readable := ReadPDF(pdf.finished())
			requireExtractionInvariants(t, document, readable)
			if !readable || document.Title != testCase.want {
				t.Fatalf("title = %t %q, want %q", readable, document.Title, testCase.want)
			}
		})
	}
}

func TestHostileInformationReferenceInACommentIsIgnored(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		comment string
	}{
		{"plain comment after the trailer", "% synthetic producer note\n"},
		{"comment naming another dictionary", "% /Info 41 0 R\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := newSyntheticPDF().
				page(1, "/Contents 2 0 R").
				rawStream(2, "", "BT (Legitimate page words here) Tj ET").
				indirectObject(40, "<< /Title (Information title) >>").
				indirectObject(41, "<< /Title (Commented title) >>").
				trailer("<< /Info 40 0 R >>").
				text(testCase.comment).
				finished()
			document, readable := ReadPDF(body)
			requireExtractionInvariants(t, document, readable)
			if !readable || document.Title != "Information title" {
				t.Fatalf(
					"title = %t %q, want the trailer information title",
					readable,
					document.Title,
				)
			}
		})
	}
}

func TestHostilePagelessFallbackSkipsScriptAttachmentAndFormDataStreams(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name     string
		referrer string
	}{
		{"stream named by nothing but a content reader", "<< /Type /Catalog >>"},
		{"javascript action stream", "<< /Type /Catalog /OpenAction << /S /JavaScript /JS 8 0 R >> >>"},
		{"untyped embedded file stream", "<< /Type /Filespec /F (note.txt) /EF << /F 8 0 R >> >>"},
		{"xfa stream", "<< /Type /Catalog /AcroForm << /Fields [] /XFA [(template) 8 0 R] >> >>"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := newSyntheticPDF().
				indirectObject(20, testCase.referrer).
				rawStream(2, "", "BT (Legitimate pageless words) Tj ET").
				rawStream(8, "", "BT (Referenced payload words) Tj ET").
				finished()
			document, readable := ReadPDF(body)
			requireExtractionInvariants(t, document, readable)
			requireTextContains(t, document.Text, "Legitimate pageless words")
			referencedAsNonContent := strings.Contains(testCase.referrer, "8 0 R")
			if extracted := strings.Contains(
				document.Text,
				"Referenced payload words",
			); extracted == referencedAsNonContent {
				t.Fatalf("referenced as non-content %t, payload extracted %t: %q",
					referencedAsNonContent, extracted, document.Text)
			}
		})
	}
}

func TestHostileNestedContentsKeyKeepsThePageContents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		page string
	}{
		{
			"inline annotation with a text contents string",
			"/Annots [<< /Type /Annot /Subtype /Text /Contents (annotation note) >>] /Contents 2 0 R",
		},
		{
			"inline annotation with a stream reference",
			"/Annots [<< /Type /Annot /Subtype /Text /Contents 3 0 R >>] /Contents 2 0 R",
		},
		{
			"page piece dictionary with a contents entry",
			"/PieceInfo << /Editor << /Private << /Contents 3 0 R >> >> >> /Contents 2 0 R",
		},
		{
			"string value naming a contents entry",
			"/Lang (/Contents 3 0 R) /Contents 2 0 R",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := newSyntheticPDF().
				page(1, testCase.page).
				rawStream(2, "", "BT (Legitimate page words here) Tj ET").
				rawStream(3, "", "BT (Annotation stream payload) Tj ET").
				finished()
			document, readable := ReadPDF(body)
			requireExtractionInvariants(t, document, readable)
			requireTextContains(t, document.Text, "Legitimate page words here")
			requireTextOmits(t, document.Text, "Annotation stream payload")
		})
	}
}

func TestHostileEncryptionNameOutsideTheTrailerKeepsAPlainDocumentReadable(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		content string
		tail    string
	}{
		{"plain page text", "BT (Legitimate page words here) Tj ET", ""},
		{"encryption name inside the page text", "BT (Legitimate page words here /Encrypt) Tj ET", ""},
		{
			"encryption name in a comment after the trailer", "BT (Legitimate page words here) Tj ET",
			"% /Encrypt 9 0 R\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := newSyntheticPDF().
				page(1, "/Contents 2 0 R").
				rawStream(2, "", testCase.content).
				trailer("<< /Root 1 0 R >>").
				text(testCase.tail).
				finished()
			document, readable := ReadPDF(body)
			requireExtractionInvariants(t, document, readable)
			if !readable {
				t.Fatal("plain document without a trailer encryption entry is unreadable")
			}
			requireTextContains(t, document.Text, "Legitimate page words here")
		})
	}
}
