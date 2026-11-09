package yagopdf

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func writePDFObjectStream(t *testing.T, pdf *bytes.Buffer, reference int, members map[int]string) {
	t.Helper()
	numbers := make([]int, 0, len(members))
	for number := range members {
		numbers = append(numbers, number)
	}
	var header, values strings.Builder
	slices.Sort(numbers)
	for _, number := range numbers {
		fmt.Fprintf(&header, "%d %d ", number, values.Len())
		values.WriteString(members[number] + "\n")
	}
	writePDFStreamObject(t, pdf, reference,
		fmt.Sprintf("/Type /ObjStm /N %d /First %d", len(members), header.Len()),
		header.String()+values.String())
}

func TestReadPDFObjectStreamPageFontAndInformation(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	writePDFObjectStream(t, &pdf, 10, map[int]string{
		1: "<< /Title (Packed Title) >>",
		3: "<< /Type /Page /Resources << /Font << /F1 4 0 R >> >> /Contents 2 0 R >>",
		4: "<< /Type /Font /Encoding << /BaseEncoding /WinAnsiEncoding /Differences [1 /L /a /n /t /e /r] >> >>",
	})
	writePDFStreamObject(t, &pdf, 2, "",
		`BT /F1 12 Tf (\001\002\003\004\005\006\003 \001\002\003\004\005\006\003) Tj ET`)
	pdf.WriteString("trailer\n<< /Info 1 0 R >>\n%%EOF")
	document, parsed := ReadPDF(pdf.Bytes())
	if !parsed || document.Title != "Packed Title" || document.Text != "Lantern Lantern" {
		t.Fatalf("object stream document = %t %+v", parsed, document)
	}
}

func TestPDFObjectStreamsLeaveOtherBodiesUnchanged(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	writePDFStreamObject(t, &pdf, 2, "", "BT (Plain body text) Tj ET")
	writePDFStreamObject(t, &pdf, 5, "/Type 7 0 R", "1 0 <<>>")
	writePDFStreamObject(t, &pdf, 6, "/Type /XRef /N 1 /First 4", "1 0 <<>>")
	writePDFStreamObject(t, &pdf, 7, "/Type /ObjStm /First 4", "1 0 <<>>")
	writePDFStreamObject(t, &pdf, 8, "/Type /ObjStm /N 1", "1 0 <<>>")
	writePDFStreamObject(t, &pdf, 9, "/Type /ObjStm /N 1 /First 40", "1 0 <<>>")
	pdf.WriteString("%%EOF")
	body := pdf.Bytes()
	quota := newPDFDecodeQuota(pdfMaxDecodedDocumentBytes)
	unpacked := pdfWithUnpackedObjectStreams(body, quota)
	if &unpacked[0] != &body[0] || len(unpacked) != len(body) {
		t.Fatal("body without readable object streams must come back unchanged")
	}
	if quota.remainingBytes != pdfMaxDecodedDocumentBytes-3*len("1 0 <<>>") {
		t.Fatalf(
			"object stream decode charge = %d",
			pdfMaxDecodedDocumentBytes-quota.remainingBytes,
		)
	}
}

func TestPDFObjectStreamDecodeIsBoundedByQuota(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	writePDFObjectStream(t, &pdf, 10, map[int]string{1: "<< /Title (Packed Title) >>"})
	pdf.WriteString("trailer\n<< /Info 1 0 R >>\n%%EOF")
	body := pdf.Bytes()
	empty := newPDFDecodeQuota(0)
	if unpacked := pdfWithUnpackedObjectStreams(body, empty); !bytes.Equal(unpacked, body) {
		t.Fatal("exhausted quota must leave the object stream packed")
	}
	quota := newPDFDecodeQuota(pdfMaxDecodedDocumentBytes)
	unpacked := pdfWithUnpackedObjectStreams(body, quota)
	if pdfInfoTitle(unpacked) != "Packed Title" {
		t.Fatalf("unpacked title = %q", pdfInfoTitle(unpacked))
	}
	decodedLength := len("1 0 ") + len("<< /Title (Packed Title) >>\n")
	if quota.remainingBytes != pdfMaxDecodedDocumentBytes-decodedLength {
		t.Fatalf(
			"object stream decode charge = %d",
			pdfMaxDecodedDocumentBytes-quota.remainingBytes,
		)
	}
}

func TestPDFObjectStreamLaterDefinitionWins(t *testing.T) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	writePDFObjectStream(t, &pdf, 10, map[int]string{1: "<< /Title (Early Title) >>"})
	writePDFObjectStream(t, &pdf, 11, map[int]string{1: "<< /Title (Middle Title) >>"})
	writePDFObjectStream(t, &pdf, 10, map[int]string{1: "<< /Title (Final Title) >>"})
	pdf.WriteString("trailer\n<< /Info 1 0 R >>\n%%EOF")
	unpacked := pdfWithUnpackedObjectStreams(
		pdf.Bytes(),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if title := pdfInfoTitle(unpacked); title != "Final Title" {
		t.Fatalf("incremental object stream title = %q", title)
	}
}

func TestPDFObjectStreamMembersShareTheObjectLimit(t *testing.T) {
	filling := make(map[int]string, pdfMaxIndirectObjects)
	for number := 1; number <= pdfMaxIndirectObjects; number++ {
		filling[number+100] = "null"
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	writePDFObjectStream(t, &pdf, 10, filling)
	writePDFObjectStream(t, &pdf, 11, map[int]string{1: "<< /Title (Over Limit) >>"})
	pdf.WriteString("%%EOF")
	unpacked := pdfWithUnpackedObjectStreams(
		pdf.Bytes(),
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
	if !bytes.Contains(unpacked, []byte("\n4196 0 obj\nnull\nendobj\n")) {
		t.Fatal("members within the object limit must unpack")
	}
	if bytes.Contains(unpacked, []byte("\n1 0 obj\n")) {
		t.Fatal("members beyond the object limit must stay packed")
	}
}

func TestPDFObjectStreamMembersRefuseMalformedHeaders(t *testing.T) {
	cases := []struct {
		name       string
		dictionary string
		decoded    string
		limit      int
		want       []string
	}{
		{
			"two members",
			"/N 2 /First 9",
			" 4 0\n5 4 null [1 2]\x00",
			8,
			[]string{"4 0=null", "5 0=[1 2]"},
		},
		{"limit caps members", "/N 2 /First 8", "4 0 5 4 null [1 2]", 1, []string{"4 0=null"}},
		{"limit reached", "/N 2 /First 8", "4 0 5 4 null [1 2]", 0, nil},
		{"missing count", "/First 4", "4 0 null", 8, nil},
		{"missing first", "/N 1", "4 0 null", 8, nil},
		{"first beyond data", "/N 1 /First 99", "4 0 null", 8, nil},
		{"non-numeric number", "/N 1 /First 4", "x 0 null", 8, nil},
		{"missing offset", "/N 1 /First 2", "4 null", 8, nil},
		{"offset beyond data", "/N 1 /First 4", "4 9 null", 8, nil},
		{"descending offsets", "/N 2 /First 8", "4 5 5 0 null [1 2]", 8, []string{"4 0=[1 2]"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			members := pdfObjectStreamMembers(
				[]byte("<< "+testCase.dictionary+" >>"),
				[]byte(testCase.decoded),
				testCase.limit,
			)
			got := make([]string, 0, len(members))
			for _, member := range members {
				got = append(got, member.reference+"="+string(member.value))
			}
			if strconv.Quote(
				strings.Join(got, "|"),
			) != strconv.Quote(
				strings.Join(testCase.want, "|"),
			) {
				t.Fatalf("members = %q, want %q", got, testCase.want)
			}
		})
	}
}
