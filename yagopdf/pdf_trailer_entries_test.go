package yagopdf

import (
	"bytes"
	"testing"
)

func TestPDFTrailerEntriesIgnoreOtherScopes(t *testing.T) {
	for _, key := range []string{"Encrypt", "Info", "Root", "ID"} {
		for _, tail := range []string{
			"% /" + key + " 9 0 R\n",
			"(/" + key + " 9 0 R)\n",
			"% trailer << /" + key + " 9 0 R >>\n",
			"(trailer << /" + key + " 9 0 R >>)\n",
			"9 0 obj\n<< /" + key + " 9 0 R >>\nendobj\n",
			"trailer << /PieceInfo << /" + key + " 9 0 R >> >>\n",
			"trailer << /List [/" + key + " 9 0 R] >>\n",
			"trailer << /Note (/" + key + " 9 0 R) >>\n",
		} {
			body := []byte("%PDF-1.7\ntrailer << /" + key + " 1 0 R >>\n" + tail)
			entry, exists := pdfLastTrailerEntry(body, key)
			if !exists || string(entry) != "1 0 R" {
				t.Fatalf("key %s tail %q: %t %q", key, tail, exists, entry)
			}
			entry, exists = pdfLastTrailerEntry([]byte("%PDF-1.7\n"+tail), key)
			if exists || entry != nil {
				t.Fatalf("non-trailer key %s tail %q selected %q", key, tail, entry)
			}
		}
	}
}

func TestPDFTrailerEntriesUseLatestTrailerAndCrossReferenceDictionary(t *testing.T) {
	pdf := newSyntheticPDF().
		trailer("<< /Info 1 0 R /Encrypt 3 0 R /ID [<010203> <04>] >>").
		rawStream(8, "/Type /XRef /Info 2 0 R /Encrypt 4 0 R /ID [<050607> <08>]", "")
	body := pdf.finished()
	for key, want := range map[string]string{"Info": "2 0 R", "Encrypt": "4 0 R"} {
		entry, exists := pdfLastTrailerEntry(body, key)
		if !exists || string(entry) != want {
			t.Fatalf("latest xref %s = %t %q", key, exists, entry)
		}
	}
	if identifier := pdfFirstDocumentIdentifier(body); !bytes.Equal(identifier, []byte{5, 6, 7}) {
		t.Fatalf("latest identifier = %x", identifier)
	}
	body = pdf.trailer("<< /Info 5 0 R /Encrypt null /ID [(final)] >>").finished()
	entry, exists := pdfLastTrailerEntry(body, "Encrypt")
	if !exists || string(entry) != "null" {
		t.Fatalf("newest explicit null = %t %q", exists, entry)
	}
	if identifier := pdfFirstDocumentIdentifier(body); string(identifier) != "final" {
		t.Fatalf("latest identifier = %q", identifier)
	}
}

func TestPDFTrailerEntriesIgnoreStreamPayloadAndNestedXRefType(t *testing.T) {
	for _, dictionary := range []string{"", "/Note << /Type /XRef >>", "/Type /Image"} {
		body := newSyntheticPDF().
			trailer("<< /Info 1 0 R /Encrypt 2 0 R /ID [(real)] >>").
			rawStream(8, dictionary+" /Info 9 0 R /Encrypt 9 0 R /ID [(false)]",
				"trailer << /Info 9 0 R /Encrypt 9 0 R /ID [(false)] >>").
			finished()
		entry, exists := pdfLastTrailerEntry(body, "Info")
		if !exists || string(entry) != "1 0 R" ||
			string(pdfFirstDocumentIdentifier(body)) != "real" {
			t.Fatalf("stream dictionary %q redirected trailer: %t %q", dictionary, exists, entry)
		}
	}
}

func TestPDFInformationTitleUsesOnlyADepthOneString(t *testing.T) {
	for _, entries := range []string{
		"/Note (/Title (False title)) /Title (Real title)",
		"/Piece << /Title (False title) >> /Title (Real title)",
		"/Array [/Title (False title)] /Title (Real title)",
		"% /Title (False title)\n/Title (Real title)",
		"/TitleSuffix (False title) /Title (Real title)",
		"/T#69tle (Real title)",
	} {
		body := newSyntheticPDF().indirectObject(1, "<< "+entries+" >>").
			trailer("<< /Info 1 0 R >>").finished()
		if title := pdfInfoTitle(body); title != "Real title" {
			t.Fatalf("entries %q: title = %q", entries, title)
		}
	}
	for _, value := range []string{"null", "[ (False title) ]", "<< /Nested (False title) >>", "/Name (False title)"} {
		body := newSyntheticPDF().indirectObject(1, "<< /Title "+value+" >>").
			trailer("<< /Info 1 0 R >>").finished()
		if title := pdfInfoTitle(body); title != "" {
			t.Fatalf("non-string %q became title %q", value, title)
		}
	}
}
