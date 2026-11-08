package extractfetch

import (
	"bytes"
	"compress/zlib"
	"context"
	"testing"
)

func pdfBody(t *testing.T, title, content string) string {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n1 0 obj\n<< /Title (" + title + ") >>\nendobj\n")
	pdf.WriteString("3 0 obj\n<< /Type /Page /Contents 2 0 R >>\nendobj\n")
	pdf.WriteString("2 0 obj\n<< /Filter /FlateDecode >>\nstream\r\n")
	pdf.Write(compressed.Bytes())
	pdf.WriteString("\nendstream\nendobj\n%%EOF")

	return pdf.String()
}

func TestFetcherReadsPDFTitleAndText(t *testing.T) {
	body := pdfBody(t, "Installation guide", "BT (Mount the camera bracket) Tj ET")
	fetcher := New(htmlClient(body, "application/pdf"), 0, 0)
	content, err := fetcher.Fetch(context.Background(), "https://example.com/guide.pdf")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if content.Title != "Installation guide" || content.Text != "Mount the camera bracket" ||
		content.Images != nil {
		t.Fatalf("content = %+v", content)
	}
	page, err := fetcher.FetchPage(context.Background(), "https://example.com/guide.pdf")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if page.Title != "Installation guide" || page.Text != "Mount the camera bracket" ||
		page.Links != nil {
		t.Fatalf("page = %+v", page)
	}
}

func TestFetcherRejectsUnreadablePDF(t *testing.T) {
	fetcher := New(htmlClient("%PDF-1.4\nno text streams\n%%EOF", "application/pdf"), 0, 0)
	if _, err := fetcher.Fetch(context.Background(), "https://example.com/a.pdf"); err == nil {
		t.Fatal("a PDF without readable text must fail")
	}
}

func TestFetcherBoundsPDFResponse(t *testing.T) {
	body := pdfBody(t, "", "BT (Bounded document text) Tj ET")
	limit := int64(len(body))
	content, err := New(htmlClient(body, "application/pdf"), 0, limit).
		Fetch(context.Background(), "https://example.com/a.pdf")
	if err != nil || content.Text != "Bounded document text" {
		t.Fatalf("exact-limit PDF = %+v, %v", content, err)
	}
	if _, err := New(htmlClient(body, "application/pdf"), 0, limit-1).
		Fetch(context.Background(), "https://example.com/a.pdf"); err == nil {
		t.Fatal("a PDF one byte over the limit must fail")
	}
}
