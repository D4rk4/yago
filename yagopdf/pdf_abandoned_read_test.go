package yagopdf

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	abandonedReadPageText      = "Readable harbour survey text for the caller"
	abandonedReadPromptStop    = 500 * time.Millisecond
	abandonedReadExpiredBudget = 20 * time.Millisecond
	abandonedReadStreams       = 64
	abandonedReadStreamBytes   = 512 << 10
)

func cancelledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	return ctx
}

func slowSyntheticPDF() []byte {
	operators := syntheticRepeatedZlib([]byte("q Q "), abandonedReadStreamBytes)
	references := make([]string, 0, abandonedReadStreams)
	pdf := newSyntheticPDF()
	for index := range abandonedReadStreams {
		references = append(references, fmt.Sprintf("%d 0 R", index+2))
		pdf.encodedStream(index+2, "/Filter /FlateDecode", operators)
	}

	return pdf.page(1, "/Contents ["+strings.Join(references, " ")+"]").finished()
}

func TestReadPDFContextReadsForALiveCallerAndRefusesACancelledOne(t *testing.T) {
	body := singlePagePDF("BT (" + abandonedReadPageText + ") Tj ET")
	document, err := ReadPDFContext(t.Context(), body)
	if err != nil || document.Text != abandonedReadPageText {
		t.Fatalf("live read = %+v, %v", document, err)
	}
	document, err = ReadPDFContext(cancelledContext(t), body)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrNoReadableText) ||
		document != (Document{}) {
		t.Fatalf("cancelled read = %+v, %v", document, err)
	}
}

func TestReadPDFContextReportsUnreadableTextApartFromAbandonment(t *testing.T) {
	for name, body := range map[string][]byte{
		"not a PDF":       []byte("plain words that are not a PDF document"),
		"empty page":      singlePagePDF("BT ET"),
		"too few letters": singlePagePDF("BT (ab) Tj ET"),
	} {
		t.Run(name, func(t *testing.T) {
			document, err := ReadPDFContext(t.Context(), body)
			if !errors.Is(err, ErrNoReadableText) || errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) || document != (Document{}) {
				t.Fatalf("unreadable read = %+v, %v", document, err)
			}
		})
	}
}

func TestReadPostScriptContextSeparatesTextUnreadableAndAbandoned(t *testing.T) {
	body := []byte("%!PS\n(" + abandonedReadPageText + ") show\n")
	document, err := ReadPostScriptContext(t.Context(), body)
	if err != nil || document.Text != abandonedReadPageText {
		t.Fatalf("live read = %+v, %v", document, err)
	}
	if _, err := ReadPostScriptContext(
		cancelledContext(t),
		body,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled read error = %v", err)
	}
	_, err = ReadPostScriptContext(t.Context(), []byte("%!PS\n(no show here)\n"))
	if !errors.Is(err, ErrNoReadableText) || errors.Is(err, context.Canceled) {
		t.Fatalf("unreadable read error = %v", err)
	}
}

func TestReadPDFContextStopsALongReadPromptlyAtItsDeadline(t *testing.T) {
	body := slowSyntheticPDF()
	ctx, cancel := context.WithTimeout(t.Context(), abandonedReadExpiredBudget)
	defer cancel()
	started := time.Now()
	document, err := ReadPDFContext(ctx, body)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) || document != (Document{}) {
		t.Fatalf("expired read = %+v, %v", document, err)
	}
	if elapsed > abandonedReadPromptStop {
		t.Fatalf("read continued %v after a %v budget", elapsed, abandonedReadExpiredBudget)
	}
	generous, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	small := singlePagePDF("BT (" + abandonedReadPageText + ") Tj ET")
	if document, err := ReadPDFContext(generous, small); err != nil ||
		!strings.Contains(document.Text, abandonedReadPageText) {
		t.Fatalf("read within budget = %+v, %v", document, err)
	}
}

func TestPDFTextCollectorObservesAbandonmentWithinOneCheckInterval(t *testing.T) {
	abandoned := make(chan struct{})
	collector := newPDFTextCollector(pdfMaxTextBytes)
	collector.abandoned = abandoned
	if collector.complete() {
		t.Fatal("an open caller must not complete the collector")
	}
	close(abandoned)
	calls := 0
	for !collector.complete() {
		calls++
		if calls > pdfAbandonmentCheckInterval {
			t.Fatalf("abandonment unseen after %d checks", calls)
		}
	}
	collector.writeString("late text")
	if collector.String() != "" {
		t.Fatalf("abandoned collector wrote %q", collector.String())
	}
}

func TestPDFDecodeQuotaRefusesDecodingOnceAbandoned(t *testing.T) {
	abandoned := make(chan struct{})
	quota := newPDFDecodeQuota(pdfMaxDecodedDocumentBytes)
	quota.abandoned = abandoned
	stream := pdfEncodedStream{encoded: []byte("BT (words) Tj ET")}
	if decoded, ok := quota.decode(stream); !ok || string(decoded) != "BT (words) Tj ET" ||
		quota.exhausted() {
		t.Fatalf("open caller decode = %q, %t", decoded, ok)
	}
	close(abandoned)
	if decoded, ok := quota.decode(stream); ok || len(decoded) != 0 || !quota.exhausted() {
		t.Fatalf("abandoned decode = %q, %t", decoded, ok)
	}
}
