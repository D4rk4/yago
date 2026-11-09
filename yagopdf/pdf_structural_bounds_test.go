package yagopdf

import (
	"fmt"
	"strings"
	"testing"
)

func TestPDFStreamRecoveryIgnoresEndstreamInsideLongerWords(t *testing.T) {
	stream, exists := pdfEncodedStreamOf(
		[]byte("<< >>\nstream\nfriendstreams endstreamy\nendstream\nendobj"),
	)
	if !exists || string(stream.encoded) != "friendstreams endstreamy\n" {
		t.Fatalf("recovered stream = %t %q", exists, stream.encoded)
	}
	if _, exists := pdfEncodedStreamOf([]byte("<< >>\nstream\nendstreamy")); exists {
		t.Fatal("a stream without a standalone endstream keyword must be refused")
	}
}

func TestPDFPageStreamLimitStopsInsideALaterPage(t *testing.T) {
	const streamsPerPage = pdfMaxStreams/2 + 8
	pdf := newSyntheticPDF().
		indirectObject(1, "<< /Type /Catalog /Pages 2 0 R >>").
		indirectObject(2, "<< /Type /Pages /Kids [3 0 R 4 0 R] >>")
	for page, first := range []int{100, 100 + streamsPerPage} {
		references := make([]string, 0, streamsPerPage)
		for index := range streamsPerPage {
			references = append(references, fmt.Sprintf("%d 0 R", first+index))
			pdf.rawStream(first+index, "", fmt.Sprintf("BT (page %d part %d) Tj ET", page, index))
		}
		pdf.page(3+page, "/Parent 2 0 R /Contents ["+strings.Join(references, " ")+"]")
	}
	streams := pdfPageDescriptionStreams(pdf.trailer("<< /Root 1 0 R >>").finished())
	if len(streams) != pdfMaxStreams {
		t.Fatalf("page streams = %d, want %d", len(streams), pdfMaxStreams)
	}
	if streams[0].scope != "page 3 0" || streams[len(streams)-1].scope != "page 4 0" {
		t.Fatalf("stream scopes = %q ... %q", streams[0].scope, streams[len(streams)-1].scope)
	}
}

func TestPDFFormQueueStopsAtTheIndirectObjectLimit(t *testing.T) {
	resources := []byte("<< /XObject << /Fm1 7 0 R >> >>")
	lookup := newPDFObjectLookup(nil)
	queued := make(map[pdfFormIdentity]struct{}, pdfMaxIndirectObjects)
	if queue := pdfQueueForms(nil, resources, lookup, queued); len(queue) != 1 ||
		queue[0].reference != "7 0" {
		t.Fatalf("form queue below the limit = %+v", queue)
	}
	for index := len(queued); index < pdfMaxIndirectObjects; index++ {
		queued[pdfFormIdentity{reference: fmt.Sprintf("%d 0", 1000+index)}] = struct{}{}
	}
	other := []byte("<< /XObject << /Fm2 8 0 R >> >>")
	if queue := pdfQueueForms(nil, other, lookup, queued); len(queue) != 0 {
		t.Fatalf("form queue at the limit = %+v", queue)
	}
}
