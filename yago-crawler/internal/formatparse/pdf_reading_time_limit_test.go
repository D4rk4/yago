package formatparse

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagopdf"
)

const (
	slowPDFStreams     = 64
	slowPDFStreamBytes = 512 << 10
	slowPDFTimeLimit   = 20 * time.Millisecond
	slowPDFPromptStop  = 500 * time.Millisecond
)

func slowSyntheticPDF(t *testing.T) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("q Q "), slowPDFStreamBytes/4)); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	var pdf, references bytes.Buffer
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

	return pdf.Bytes()
}

func TestPDFReadingTimeLimitStopsALongReadAndAdmitsAShortOne(t *testing.T) {
	started := time.Now()
	_, err := readPDFOrPostScriptWithin(slowPDFTimeLimit, "https://a.example/slow.pdf",
		slowSyntheticPDF(t))
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, yagopdf.ErrNoReadableText) {
		t.Fatalf("expired PDF read error = %v", err)
	}
	if elapsed > slowPDFPromptStop {
		t.Fatalf("PDF reading continued %v after a %v limit", elapsed, slowPDFTimeLimit)
	}
	document, err := readPDFOrPostScriptWithin(time.Minute, "https://a.example/report.pdf",
		pdfWithText(t, "Report", "BT /F1 12 Tf (Quarterly harbour survey) Tj ET"))
	if err != nil || document.Text != "Quarterly harbour survey" {
		t.Fatalf("PDF read within its limit = %+v, %v", document, err)
	}
}

func TestPostScriptReadingTimeLimitRefusesAnExpiredReadAndAdmitsALiveOne(t *testing.T) {
	body := []byte("%!PS\n(Quarterly harbour survey) show\n")
	if _, err := readPDFOrPostScriptWithin(0, "https://a.example/survey.ps", body); !errors.Is(
		err, context.DeadlineExceeded) {
		t.Fatalf("expired PostScript read error = %v", err)
	}
	document, err := readPDFOrPostScriptWithin(time.Minute, "https://a.example/survey.ps", body)
	if err != nil || document.Text != "Quarterly harbour survey" {
		t.Fatalf("PostScript read within its limit = %+v, %v", document, err)
	}
}

func TestPDFReadingTimeLimitReportsUnreadableTextApartFromExpiry(t *testing.T) {
	for name, body := range map[string][]byte{
		"blank PDF":         pdfWithText(t, "Blank", "BT ET"),
		"silent PostScript": []byte("%!PS\n(no show here)\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readPDFOrPostScriptWithin(time.Minute, "https://a.example/blank", body)
			if !errors.Is(err, yagopdf.ErrNoReadableText) ||
				errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("unreadable read error = %v", err)
			}
		})
	}
}
