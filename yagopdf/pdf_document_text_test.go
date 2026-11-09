package yagopdf

import (
	"strings"
	"testing"
)

func TestPDFContentStreamsSharePageTextState(t *testing.T) {
	body := newSyntheticPDF().
		page(1, "/Contents [2 0 R 3 0 R]").
		rawStream(2, "", "BT (First page segment) Tj").
		rawStream(3, "", "(second page segment) Tj ET").
		page(4, "/Contents 5 0 R").
		rawStream(5, "", "(outside the next page block) Tj BT (Next page words) Tj ET").
		finished()
	document, readable := ReadPDF(body)
	if !readable || document.Text != "First page segment second page segment\nNext page words" {
		t.Fatalf("split content text = %t %q", readable, document.Text)
	}
}

func TestPDFRepeatedPageContentStartsEachPage(t *testing.T) {
	body := newSyntheticPDF().
		page(1, "/Contents [2 0 R 3 0 R]").
		page(4, "/Contents [2 0 R 5 0 R]").
		rawStream(2, "", "BT (Shared page prefix) Tj").
		rawStream(3, "", "(first ending words) Tj ET").
		rawStream(5, "", "(second ending words) Tj ET").
		finished()
	document, readable := ReadPDF(body)
	if !readable ||
		document.Text != "Shared page prefix first ending words\nShared page prefix second ending words" {
		t.Fatalf("shared content text = %t %q", readable, document.Text)
	}
}

func TestPDFPageWithoutResourcesDoesNotUseAnotherPageFont(t *testing.T) {
	body := newSyntheticPDF().
		page(1, "/Resources << /Font << /F1 6 0 R >> >> /Contents 2 0 R").
		indirectObject(6, "<< /Type /Font /Encoding /WinAnsiEncoding >>").
		rawStream(2, "", "BT /F1 12 Tf (Legitimate page words) Tj ET").
		page(3, "/Contents 4 0 R").
		rawStream(4, "", "BT /F1 12 Tf (Unscoped font words) Tj ET").
		finished()
	document, readable := ReadPDF(body)
	if !readable || document.Text != "Legitimate page words" {
		t.Fatalf("unscoped font text = %t %q", readable, document.Text)
	}
}

func TestPDFPageTextRespectsOutputLimitAcrossStreams(t *testing.T) {
	body := newSyntheticPDF().page(1, "/Contents [2 0 R 3 0 R]").
		rawStream(2, "", "BT (First bounded words) Tj ET").
		rawStream(3, "", "BT (Second unseen words) Tj ET").finished()
	text := pdfPageText(body, newPDFDecodeQuota(pdfMaxDecodedDocumentBytes), 12)
	if text != "First bounde" || strings.Contains(text, "Second") {
		t.Fatalf("bounded page text = %q", text)
	}
}
