package formatparse

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func writePDFStreamObject(
	t *testing.T,
	pdf *bytes.Buffer,
	reference int,
	dictionary string,
	content string,
) {
	t.Helper()
	compressed := zlibBytes(t, content)
	fmt.Fprintf(pdf, "%d 0 obj\n<< %s /Filter /FlateDecode /Length %d >>\nstream\n",
		reference, dictionary, len(compressed))
	pdf.Write(compressed)
	pdf.WriteString("\nendstream\nendobj\n")
}

func TestPDFPageDescriptionSkipsBinaryObjectsAndKeepsToUnicode(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	pdf.WriteString("1 0 obj\n<< /Type /Page /Contents 2 0 R " +
		"/Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	writePDFStreamObject(t, &pdf, 2, "", `BT /F1 12 Tf (\002rmly useful page text) Tj ET`)
	writePDFStreamObject(t, &pdf, 3, "/Type /XObject /Subtype /Image",
		`BT (image pixels disguised as text) Tj ET`)
	writePDFStreamObject(t, &pdf, 4, "", `BT (embedded font payload disguised as text) Tj ET`)
	pdf.WriteString("5 0 obj\n<< /Type /Font /ToUnicode 6 0 R " +
		"/FontDescriptor 7 0 R >>\nendobj\n")
	writePDFStreamObject(t, &pdf, 6, "", subsetCMap)
	pdf.WriteString("7 0 obj\n<< /Type /FontDescriptor /FontFile2 4 0 R >>\nendobj\n%%EOF")

	streams := pdfPageDescriptionStreams(pdf.Bytes())
	if len(streams) != 1 {
		t.Fatalf("selected page streams = %d, want 1", len(streams))
	}
	page, parsed := parsePDF("https://a.example/slides.pdf", "application/pdf", pdf.Bytes())
	if !parsed || !strings.Contains(page.Text, "firmly useful page text") {
		t.Fatalf("structured pdf = %v %q", parsed, page.Text)
	}
	for _, unwanted := range []string{"image pixels", "embedded font payload"} {
		if strings.Contains(page.Text, unwanted) {
			t.Fatalf("non-page payload %q leaked into %q", unwanted, page.Text)
		}
	}
}

func TestPDFPageDescriptionResolvesArraysAndForms(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	pdf.WriteString("1 0 obj\n<< /Type /Page /Contents 2 0 R " +
		"/Resources << /XObject << /Section 5 0 R >> >> >>\nendobj\n")
	pdf.WriteString("2 0 obj\n[3 0 R 4 0 R]\nendobj\n")
	writePDFStreamObject(t, &pdf, 3, "", `BT (first page section) Tj ET`)
	writePDFStreamObject(t, &pdf, 4, "", `BT (second page section) Tj ET`)
	writePDFStreamObject(t, &pdf, 5, "/Type /XObject /Subtype /Form",
		`BT (form page section) Tj ET`)
	writePDFStreamObject(t, &pdf, 6, "/Type /ObjStm",
		`BT (object container payload) Tj ET`)
	pdf.WriteString("%%EOF")

	page, parsed := parsePDF("https://a.example/sections.pdf", "application/pdf", pdf.Bytes())
	if !parsed {
		t.Fatal("page with indirect content array must parse")
	}
	for _, expected := range []string{"first page section", "second page section", "form page section"} {
		if !strings.Contains(page.Text, expected) {
			t.Fatalf("page text missing %q in %q", expected, page.Text)
		}
	}
	if strings.Contains(page.Text, "object container payload") {
		t.Fatalf("object stream leaked into %q", page.Text)
	}
}

func TestPDFPageDescriptionFollowsReachableNestedForms(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.7\n")
	pdf.WriteString("1 0 obj\n<< /Type /Page /Parent 10 0 R /Contents 2 0 R >>\nendobj\n")
	writePDFStreamObject(t, &pdf, 2, "", `BT (page content section) Tj ET`)
	writePDFStreamObject(t, &pdf, 3,
		"/Type /XObject /Subtype /Form /Resources << /XObject << /Nested 4 0 R >> >>",
		`BT (outer form section) Tj ET`)
	writePDFStreamObject(t, &pdf, 4, "/Type /XObject /Subtype /Form",
		`BT (nested form section) Tj ET`)
	writePDFStreamObject(t, &pdf, 5, "/Type /XObject /Subtype /Form",
		`BT (unreferenced form payload) Tj ET`)
	writePDFStreamObject(t, &pdf, 6, "/Type /XObject /Subtype /Image",
		`BT (referenced image payload) Tj ET`)
	pdf.WriteString("10 0 obj\n<< /Type /Pages /Resources 11 0 R >>\nendobj\n")
	pdf.WriteString("11 0 obj\n<< /XObject 12 0 R >>\nendobj\n")
	pdf.WriteString("12 0 obj\n<< /Outer 3 0 R /Image 6 0 R >>\nendobj\n%%EOF")

	page, parsed := parsePDF("https://a.example/forms.pdf", "application/pdf", pdf.Bytes())
	if !parsed {
		t.Fatal("page with nested forms did not parse")
	}
	for _, expected := range []string{
		"page content section",
		"outer form section",
		"nested form section",
	} {
		if !strings.Contains(page.Text, expected) {
			t.Fatalf("reachable page text missing %q in %q", expected, page.Text)
		}
	}
	for _, unwanted := range []string{"unreferenced form payload", "referenced image payload"} {
		if strings.Contains(page.Text, unwanted) {
			t.Fatalf("non-page payload %q leaked into %q", unwanted, page.Text)
		}
	}
}

func TestPDFUnreferencedFormsCannotStarvePageContent(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.7\n")
	for index := 0; index < pdfMaxStreams+1; index++ {
		writePDFStreamObject(t, &pdf, 100+index, "/Type /XObject /Subtype /Form",
			`BT (unreferenced form payload) Tj ET`)
	}
	pdf.WriteString("1 0 obj\n<< /Type /Page /Contents 2 0 R >>\nendobj\n")
	writePDFStreamObject(t, &pdf, 2, "", `BT (late page content survives) Tj ET`)
	pdf.WriteString("%%EOF")

	page, parsed := parsePDF("https://a.example/form-cap.pdf", "application/pdf", pdf.Bytes())
	if !parsed || !strings.Contains(page.Text, "late page content survives") {
		t.Fatalf("cap-starved page = %t/%q", parsed, page.Text)
	}
	if strings.Contains(page.Text, "unreferenced form payload") {
		t.Fatalf("unreferenced forms leaked into %q", page.Text)
	}
}

func TestPDFIndirectObjectSyntaxAndBounds(t *testing.T) {
	oversized := make([]byte, pdfMaxObjScanBytes+1)
	copy(oversized, []byte("1 0 obj\n<< /Value /First >>\nendobj\n"))
	objects := pdfIndirectObjects(oversized)
	if len(objects) != 1 || !bytes.Contains(objects[0].value, []byte("First")) {
		t.Fatalf("bounded object scan = %+v", objects)
	}

	incremental := []byte("1 0 obj\n<< /Value /Old >>\nendobj\r" +
		"1 0 obj\n<< /Value /New >>\nendobj\r")
	objects = pdfIndirectObjects(incremental)
	if len(objects) != 1 || !bytes.Contains(objects[0].value, []byte("New")) {
		t.Fatalf("incremental object = %+v", objects)
	}
	if got := pdfIndirectObjects([]byte("1 0 obj\n<<")); len(got) != 0 {
		t.Fatalf("unterminated object scan = %+v", got)
	}
	if end := pdfIndirectObjectEnd([]byte("<< /Title (mainstream) >>\nendobj")); end < 0 {
		t.Fatal("name suffix must not become a stream keyword")
	}

	for _, malformed := range [][]byte{
		[]byte("no terminator"),
		[]byte("stream\npayload\nendobj"),
		[]byte("stream\npayload\nendobj\nendstream"),
	} {
		if end := pdfIndirectObjectEnd(malformed); end != -1 {
			t.Fatalf("malformed object end = %d for %q", end, malformed)
		}
	}

	streamValue := []byte("<< >>\nstream\r\npayload\nendstream")
	stream, ok := pdfEncodedStreamOf(streamValue)
	if !ok || string(stream.encoded) != "payload\n" {
		t.Fatalf("CRLF stream = %v %q", ok, stream.encoded)
	}
	if _, ok := pdfEncodedStreamOf([]byte("<< >>")); ok {
		t.Fatal("streamless object must not select")
	}
	if _, ok := pdfEncodedStreamOf([]byte("stream\npayload")); ok {
		t.Fatal("unterminated stream must not select")
	}

	manyObjects := make([]pdfIndirectObject, pdfMaxStreams+1)
	selected := make([]string, 0, len(manyObjects))
	byReference := make(map[string]pdfIndirectObject, len(manyObjects))
	for index := range manyObjects {
		reference := fmt.Sprintf("%d 0", index+1)
		manyObjects[index] = pdfIndirectObject{reference: reference, value: streamValue}
		selected = append(selected, reference)
		byReference[reference] = manyObjects[index]
	}
	lookup := pdfObjectLookup{objects: manyObjects, byReference: byReference}
	if got := pdfSelectedObjectStreams(lookup, selected); len(got) != pdfMaxStreams {
		t.Fatalf("selected stream bound = %d", len(got))
	}
	if got := pdfFallbackDescriptionStreams(nil, manyObjects); len(got) != pdfMaxStreams {
		t.Fatalf("fallback stream bound = %d", len(got))
	}
}

func TestPDFPageContentsSyntaxEdges(t *testing.T) {
	for _, value := range [][]byte{
		nil,
		[]byte("<< /Type /Page >>"),
		[]byte("<< /Contents"),
		[]byte("<< /Contents [1 0 R"),
		[]byte("<< /Contents /None >>"),
	} {
		if references := pdfPageContentsReferences(value); len(references) != 0 {
			t.Fatalf("malformed contents references = %v for %q", references, value)
		}
	}

	streamValue := []byte("<< >>\nstream\npayload\nendstream")
	objectByReference := map[string]pdfIndirectObject{
		"1 0": {reference: "1 0", value: streamValue},
		"2 0": {reference: "2 0", value: []byte("<< /NotAnArray true >>")},
		"3 0": {reference: "3 0", value: []byte("[1 0 R")},
		"4 0": {reference: "4 0", value: []byte("[1 0 R]")},
	}
	resolved := pdfResolvedPageContentReferences(
		[]string{"1 0", "1 0", "missing 0", "2 0", "3 0", "4 0"},
		objectByReference,
	)
	if len(resolved) != 1 || resolved[0] != "1 0" {
		t.Fatalf("resolved malformed references = %v", resolved)
	}

	fanout := make(map[string]pdfIndirectObject, pdfMaxIndirectObjects)
	var root strings.Builder
	root.WriteString("[1 0 R ")
	for array := 2; array <= pdfMaxStreams; array++ {
		fmt.Fprintf(&root, "%d 0 R ", array)
	}
	root.WriteByte(']')
	fanout["1 0"] = pdfIndirectObject{reference: "1 0", value: []byte(root.String())}
	streamReference := 10_000
	for array := 2; array <= pdfMaxStreams; array++ {
		var children strings.Builder
		children.WriteByte('[')
		for child := 0; child < pdfMaxStreams; child++ {
			fmt.Fprintf(&children, "%d 0 R ", streamReference)
			if len(fanout) < pdfMaxIndirectObjects {
				reference := fmt.Sprintf("%d 0", streamReference)
				fanout[reference] = pdfIndirectObject{reference: reference, value: streamValue}
			}
			streamReference++
		}
		children.WriteByte(']')
		reference := fmt.Sprintf("%d 0", array)
		fanout[reference] = pdfIndirectObject{
			reference: reference,
			value:     []byte(children.String()),
		}
	}
	resolved = pdfResolvedPageContentReferences([]string{"1 0"}, fanout)
	if len(resolved) > pdfMaxIndirectObjects ||
		len(resolved) != pdfMaxIndirectObjects-pdfMaxStreams {
		t.Fatalf("bounded cyclic fanout = %d", len(resolved))
	}
}

func TestPDFResourceDictionarySyntaxAndBounds(t *testing.T) {
	dictionary := []byte("<< % /Resources << /Ignored 9 0 R >>\n" +
		"/Text (/Resources << /Ignored 8 0 R >>) " +
		"/Code <2F5265736F7572636573> /Noise > " +
		"/Resources << /XObject << /Form 1 0 R >> >> >>")
	lookup := pdfObjectLookup{byReference: map[string]pdfIndirectObject{}}
	resources := pdfDictionaryForEntry(dictionary, "Resources", lookup)
	if resources == nil || !bytes.Contains(resources, []byte("/Form 1 0 R")) {
		t.Fatalf("direct resources = %q", resources)
	}
	direct := pdfDirectDictionary([]byte(
		"<< % >> comment\n/Text (>> literal) /Code <3E3E> /Noise > " +
			"/Nested << /A 1 >> >>tail",
	))
	if direct == nil || bytes.HasSuffix(direct, []byte("tail")) {
		t.Fatalf("bounded direct dictionary = %q", direct)
	}
	for _, malformed := range [][]byte{
		[]byte("<"),
		[]byte("<< /Open 1"),
		[]byte("<< <AB"),
	} {
		if got := pdfDirectDictionary(malformed); got != nil {
			t.Fatalf("malformed direct dictionary = %q", got)
		}
		if got := pdfDictionaryEntryValue(malformed, "Resources"); got != nil {
			t.Fatalf("malformed dictionary entry = %q", got)
		}
	}
	if got := pdfDictionaryForEntry(
		[]byte("<< /Resources /None >>"),
		"Resources",
		lookup,
	); got != nil {
		t.Fatalf("named resources = %q", got)
	}
	if got := pdfDictionaryForEntry(
		[]byte("<< /Resources 99 0 R >>"),
		"Resources",
		lookup,
	); got != nil {
		t.Fatalf("missing indirect resources = %q", got)
	}
	if got := pdfLeadingReference([]byte("/None")); got != "" {
		t.Fatalf("named reference = %q", got)
	}

	cycleLookup := pdfObjectLookup{byReference: map[string]pdfIndirectObject{
		"1 0": {reference: "1 0", value: []byte("<< /Parent 2 0 R >>")},
		"2 0": {reference: "2 0", value: []byte("<< /Parent 1 0 R >>")},
	}}
	if got := pdfPageResourceDictionary(cycleLookup.value("1 0"), cycleLookup); got != nil {
		t.Fatalf("cyclic inherited resources = %q", got)
	}
	if got := pdfPageResourceDictionary(
		[]byte("<< /Parent 3 0 R >>"),
		cycleLookup,
	); got != nil {
		t.Fatalf("missing inherited resources = %q", got)
	}

	seen := map[string]struct{}{"1 0": {}}
	queued := pdfAppendObjectReferences(nil, []string{"1 0", "2 0"}, seen)
	if len(queued) != 1 || queued[0] != "2 0" {
		t.Fatalf("deduplicated object references = %v", queued)
	}
	for index := len(seen); index < pdfMaxIndirectObjects; index++ {
		seen[fmt.Sprintf("cap-%d", index)] = struct{}{}
	}
	if got := pdfAppendObjectReferences(queued, []string{"3 0"}, seen); len(got) != len(queued) {
		t.Fatalf("over-cap object references = %v", got)
	}
	if got := pdfAppendUniqueReferences(
		[]string{"1 0"},
		[]string{"1 0", "2 0", "3 0"},
		2,
	); len(got) != 2 || got[1] != "2 0" {
		t.Fatalf("bounded unique references = %v", got)
	}
}

func TestPDFLooseDescriptionSkipsTypedPayloads(t *testing.T) {
	body := []byte("%PDF\n<< /Subtype /Image >>stream\r\nimage\nendstream\n" +
		"<< /Filter /FlateDecode >>stream\r\npage\nendstream")
	streams := pdfLooseDescriptionStreams(body)
	if len(streams) != 1 || string(streams[0].encoded) != "page\n" {
		t.Fatalf("loose description streams = %+v", streams)
	}
}

func TestPDFFallbackDescriptionSkipsReferencedFontPrograms(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n1 0 obj\n<< /Type /FontDescriptor /FontFile2 2 0 R >>\nendobj\n")
	writePDFStreamObject(t, &pdf, 2, "", `BT (font program payload) Tj ET`)
	writePDFStreamObject(t, &pdf, 3, "/Type /XObject /Subtype /Image",
		`BT (image payload) Tj ET`)
	writePDFStreamObject(t, &pdf, 4, "", `BT (fallback page text) Tj ET`)
	pdf.WriteString("%%EOF")

	page, parsed := parsePDF("https://a.example/fallback.pdf", "application/pdf", pdf.Bytes())
	if !parsed || !strings.Contains(page.Text, "fallback page text") {
		t.Fatalf("fallback pdf = %v %q", parsed, page.Text)
	}
	for _, unwanted := range []string{"font program payload", "image payload"} {
		if strings.Contains(page.Text, unwanted) {
			t.Fatalf("fallback payload %q leaked into %q", unwanted, page.Text)
		}
	}
}
