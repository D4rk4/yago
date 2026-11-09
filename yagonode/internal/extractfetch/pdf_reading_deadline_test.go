package extractfetch

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagopdf"
)

const (
	slowPDFStreams      = 64
	slowPDFStreamBytes  = 512 << 10
	slowPDFFetchTimeout = 20 * time.Millisecond
	slowPDFPromptStop   = 500 * time.Millisecond
)

func slowSyntheticPDF(t *testing.T) string {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("q Q "), slowPDFStreamBytes/4)); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	var pdf, references strings.Builder
	pdf.WriteString("%PDF-1.4\n")
	for index := range slowPDFStreams {
		fmt.Fprintf(&references, "%d 0 R ", index+2)
		fmt.Fprintf(&pdf, "%d 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n",
			index+2, compressed.Len())
		pdf.Write(compressed.Bytes())
		pdf.WriteString("\nendstream\nendobj\n")
	}
	pdf.WriteString(
		"1 0 obj\n<< /Type /Page /Contents [" + references.String() + "] >>\nendobj\n%%EOF",
	)

	return pdf.String()
}

func TestFetcherStopsReadingAPDFAtTheFetchDeadline(t *testing.T) {
	fetcher := New(htmlClient(slowSyntheticPDF(t), "application/pdf"), slowPDFFetchTimeout, 0)
	started := time.Now()
	_, err := fetcher.Fetch(context.Background(), "https://example.com/slow.pdf")
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, yagopdf.ErrNoReadableText) {
		t.Fatalf("expired PDF fetch error = %v", err)
	}
	if elapsed > slowPDFPromptStop {
		t.Fatalf("PDF reading continued %v after a %v timeout", elapsed, slowPDFFetchTimeout)
	}
	readable := pdfBody(t, "Survey", "BT (Readable harbour survey) Tj ET")
	content, err := New(htmlClient(readable, "application/pdf"), time.Minute, 0).
		Fetch(context.Background(), "https://example.com/survey.pdf")
	if err != nil || content.Text != "Readable harbour survey" {
		t.Fatalf("PDF fetch within its timeout = %+v, %v", content, err)
	}
}

func TestFetcherReportsUnreadablePDFApartFromItsDeadline(t *testing.T) {
	unreadable := pdfBody(t, "Blank", "BT ET")
	_, err := New(htmlClient(unreadable, "application/pdf"), time.Minute, 0).
		Fetch(context.Background(), "https://example.com/blank.pdf")
	if !errors.Is(err, yagopdf.ErrNoReadableText) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unreadable PDF fetch error = %v", err)
	}
}
