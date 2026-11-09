package yagopdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestPDFDirectStreamLengthKeepsEmbeddedMarkersInsideTheStream(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		for _, payload := range []string{
			"plain stream bytes",
			"embedded endstream\nendobj\n2 0 obj\n<< /Title (False object) >>\nendobj\n",
		} {
			value := []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s%sendstream\nendobj",
				len(payload), payload, ending))
			stream, exists := pdfEncodedStreamOf(value)
			if !exists || string(stream.encoded) != payload {
				t.Fatalf("stream = %t %q, want %q", exists, stream.encoded, payload)
			}
			if end := pdfIndirectObjectEnd(value); end != len(value)-len("endobj") {
				t.Fatalf("object ends at %d, want %d", end, len(value)-len("endobj"))
			}
		}
	}
}

func TestPDFStreamLengthRecoveryUsesOnlyTheDirectLengthEntry(t *testing.T) {
	for _, entry := range []string{
		"/Length 2 0 R", "/Length -1", "/Length 99999999999999999999999",
		"/Length 9999", "/Length 2", "/Length 4.5", "/Length 4suffix", "/PieceInfo << /Length 0 >>",
		"/Note (/Length 0)", "% /Length 0\n", "/Length [0]",
	} {
		value := []byte("<< " + entry + " >>\nstream\ntext\nendstream\nendobj")
		stream, exists := pdfEncodedStreamOf(value)
		if !exists || string(stream.encoded) != "text\n" {
			t.Fatalf("entry %q stream = %t %q", entry, exists, stream.encoded)
		}
	}
	for _, length := range []string{"4", "+4"} {
		stream, exists := pdfEncodedStreamOf(
			[]byte("<< /Length " + length + " >>\nstream\ntext\nendstream\nendobj"),
		)
		if !exists || string(stream.encoded) != "text" {
			t.Fatalf("valid length %s changed = %t %q", length, exists, stream.encoded)
		}
	}
}

func TestPDFDictionaryEntriesIgnoreValuesAndNestedContainers(t *testing.T) {
	for _, prefix := range []string{
		"/Note (/Contents 9 0 R)", "/Names [/Contents 9 0 R]",
		"/Piece << /Contents 9 0 R >>", "% /Contents 9 0 R\n",
		"/Role /Contents", "/ContentsSuffix 9 0 R", "/Encoded <2f436f6e74656e7473>",
	} {
		body := []byte("<< " + prefix + " /Contents % real entry\n2 0 R >>")
		if references := pdfPageContentsReferences(body); strings.Join(references, ",") != "2 0" {
			t.Fatalf("prefix %q references = %v", prefix, references)
		}
	}
	for _, contents := range []string{"9 0 Rubbish", "[(9 0 R) << /Nested 9 0 R >>]", "null"} {
		if references := pdfPageContentsReferences(
			[]byte("<< /Contents " + contents + " >>"),
		); len(
			references,
		) != 0 {
			t.Fatalf("non-reference %q selected %v", contents, references)
		}
	}
}

func TestPDFMalformedObjectsCannotPromoteTheirApparentObjectHeaders(t *testing.T) {
	for _, opener := range []string{"<< /Value (", "<< /Value <", "<< /Value [", "<< /Value <<"} {
		fake := "\nendobj\n1 0 obj\n<< /Type /Page /Contents 9 0 R >>\nendobj\n" +
			"9 0 obj\n<< /Length 29 >>\nstream\nBT (Forged page words) Tj ET\nendstream\nendobj\n"
		for _, before := range []bool{false, true} {
			pdf := newSyntheticPDF()
			if before {
				pdf.page(1, "/Contents 2 0 R").rawStream(2, "", "BT (Legitimate page words) Tj ET")
			}
			body := pdf.text("7 0 obj\n" + opener + fake).finished()
			document, readable := ReadPDF(body)
			if readable != before || strings.Contains(document.Text, "Forged") {
				t.Fatalf(
					"opener %q previous page %t: %t %q",
					opener,
					before,
					readable,
					document.Text,
				)
			}
			if before && document.Text != "Legitimate page words" {
				t.Fatalf("previous page changed: %q", document.Text)
			}
		}
	}
}

func TestPDFStructuralObjectScannerSkipsCommentAndStringHeaders(t *testing.T) {
	fake := "1 0 obj\n<< /Title (False object) >>\nendobj\n"
	for _, prefix := range []string{"% " + strings.ReplaceAll(fake, "\n", "\n% ") + "\n", "(" + fake + ")\n"} {
		body := []byte("%PDF-1.7\n" + prefix + "2 0 obj\n<< /Title (Real object) >>\nendobj\n")
		objects := pdfScanIndirectObjects(body)
		if len(objects) != 1 || objects[0].reference != "2 0" ||
			!bytes.Contains(objects[0].value, []byte("Real object")) {
			t.Fatalf("objects = %+v", objects)
		}
	}
}
