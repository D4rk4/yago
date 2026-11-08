package formatparse

import (
	"bytes"
	"compress/zlib"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func pdfWithText(t *testing.T, title, content string) []byte {
	t.Helper()
	var z bytes.Buffer
	writer := zlib.NewWriter(&z)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n1 0 obj\n<< /Title (" + title + ") >>\nendobj\n")
	pdf.WriteString("3 0 obj\n<< /Type /Page /Resources << /Font << /F1 4 0 R >> >> " +
		"/Contents 2 0 R >>\nendobj\n")
	pdf.WriteString("4 0 obj\n<< /Type /Font /Encoding /WinAnsiEncoding >>\nendobj\n")
	pdf.WriteString("2 0 obj\n<< /Filter /FlateDecode >>\nstream\r\n")
	pdf.Write(z.Bytes())
	pdf.WriteString("\nendstream\nendobj\n%%EOF")

	return pdf.Bytes()
}

func TestParsePDFKeepsDocumentInfoTitle(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/report.pdf", "application/pdf",
		pdfWithText(t, "Doc title", "BT (Hello searchable world) Tj ET"),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Doc title" || page.Text != "Hello searchable world" ||
		page.URL != "https://a.example/report.pdf" {
		t.Fatalf("pdf parse = %v %+v", parsed, page)
	}
}

func TestParsePDFFallsBackToFirstTextLineTitle(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/untitled.pdf", "application/pdf",
		pdfWithText(t, "", "BT (Fallback title line) Tj ET"),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Fallback title line" {
		t.Fatalf("fallback title = %v %+v", parsed, page)
	}
}

func TestParsePDFRejectsUnreadableBody(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/fake.pdf", "application/pdf", []byte("not a pdf"),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if parsed || page.URL != "https://a.example/fake.pdf" || page.Text != "" {
		t.Fatalf("non-pdf body = %v %+v", parsed, page)
	}
}

func TestParsePostScriptByExtensionAndBody(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	page, parsed := Parse(
		"https://a.example/doc.ps", "application/postscript",
		[]byte("%!\n(PostScript shown text) show\n"), toggles,
	)
	if !parsed || page.Title != "PostScript shown text" {
		t.Fatalf("ps by extension = %v %+v", parsed, page)
	}
	page, parsed = parsePDF(
		"https://a.example/mislabeled.pdf", "",
		[]byte("%!PS\n(Routed as postscript literal text) show\n"),
	)
	if !parsed || !strings.Contains(page.Text, "Routed as postscript") {
		t.Fatalf("ps routing = %v %+v", parsed, page)
	}
}
