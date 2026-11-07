package crawlresults_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type scriptedConditionalRecrawl struct {
	recorded          bool
	recordErr         error
	validatorsErr     error
	notModifiedURLs   []string
	validatedBatchURL []string
}

func (r *scriptedConditionalRecrawl) RecordResponseValidators(
	_ context.Context,
	batch yagocrawlcontract.IngestBatch,
) error {
	r.validatedBatchURL = append(r.validatedBatchURL, batch.SourceURL)

	return r.validatorsErr
}

func (r *scriptedConditionalRecrawl) RecordNotModifiedObservation(
	_ context.Context,
	batch yagocrawlcontract.IngestBatch,
) (bool, error) {
	r.notModifiedURLs = append(r.notModifiedURLs, batch.SourceURL)

	return r.recorded, r.recordErr
}

type conditionalConsumerRun struct {
	conditional *scriptedConditionalRecrawl
	documents   *recordingDocumentReceiver
	urls        *recordingURLReceiver
	postings    *recordingPostingReceiver
	ownership   crawlresults.OwnershipCheck
}

func newConditionalConsumerRun(conditional *scriptedConditionalRecrawl) *conditionalConsumerRun {
	return &conditionalConsumerRun{
		conditional: conditional,
		documents:   &recordingDocumentReceiver{},
		urls:        &recordingURLReceiver{},
		postings:    &recordingPostingReceiver{},
	}
}

func (r *conditionalConsumerRun) absorb(
	t *testing.T,
	deliveries ...crawlresults.IngestDelivery,
) {
	t.Helper()
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, len(deliveries))}
	for _, delivery := range deliveries {
		stream.out <- delivery
	}
	consumer := crawlresults.NewIngestConsumer(stream, r.documents, r.urls, r.postings)
	if r.conditional != nil {
		consumer.RecordConditionalRecrawl(r.conditional)
	}
	consumer.CheckOwnership(r.ownership)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func notModifiedBatch(sourceURL string) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:     sourceURL,
		ProfileHandle: "handle-a",
		ObservationID: "not-modified-" + sourceURL,
		ObservedAt:    time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC),
		NotModified: &yagocrawlcontract.NotModifiedObservation{
			Revision: yagocrawlcontract.DocumentRevision{
				ContentHash:          "sha256:stored",
				ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
				FetchedAt:            time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
			},
		},
	}
}

func validatedDocumentBatch(sourceURL string) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:          sourceURL,
		ProfileHandle:      "handle-a",
		ObservationID:      "document-" + sourceURL,
		ResponseValidators: &yagocrawlcontract.HTTPValidators{ETag: `"first"`},
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: sourceURL,
			ExtractedText: "synthetic page text",
			ContentHash:   "sha256:stored",
		},
	}
}

func settlingDelivery(
	batch yagocrawlcontract.IngestBatch,
	settled chan<- string,
) crawlresults.IngestDelivery {
	report := func(outcome string) func(context.Context) error {
		return func(context.Context) error {
			settled <- outcome

			return nil
		}
	}

	return crawlresults.IngestDelivery{
		Batch:                  batch,
		Ack:                    report("ack"),
		Nak:                    report("nak"),
		LeaseLost:              report("lease lost"),
		AckRecordedNotModified: report("recorded"),
	}
}

func awaitSettlement(t *testing.T, settled <-chan string) string {
	t.Helper()
	select {
	case outcome := <-settled:
		return outcome
	case <-time.After(2 * time.Second):
		t.Fatal("delivery did not settle")

		return ""
	}
}

func TestNotModifiedObservationIsConfirmedOnlyAfterItIsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name        string
		conditional scriptedConditionalRecrawl
		want        string
	}{
		{name: "recorded", conditional: scriptedConditionalRecrawl{recorded: true}, want: "recorded"},
		{name: "refused", want: "ack"},
		{
			name:        "recording failed",
			conditional: scriptedConditionalRecrawl{recordErr: errors.New("vault closed")},
			want:        "nak",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := newConditionalConsumerRun(&tc.conditional)
			settled := make(chan string, 1)

			run.absorb(t, settlingDelivery(notModifiedBatch("https://example.org/page"), settled))

			if outcome := awaitSettlement(t, settled); outcome != tc.want {
				t.Fatalf("settlement = %q, want %q", outcome, tc.want)
			}
			if run.documents.calls+run.urls.calls+run.postings.calls != 0 {
				t.Fatal("not-modified observation reached the document, URL or posting stores")
			}
		})
	}
}

func TestNotModifiedSettlementFailureIsReported(t *testing.T) {
	for _, tc := range []struct {
		name         string
		conditional  scriptedConditionalRecrawl
		settleErr    error
		wantReported bool
	}{
		{name: "recorded and settled", conditional: scriptedConditionalRecrawl{recorded: true}},
		{
			name:         "recorded but unsettled",
			conditional:  scriptedConditionalRecrawl{recorded: true},
			settleErr:    errors.New("acknowledgement lost"),
			wantReported: true,
		},
		{name: "refused and settled"},
		{
			name:         "refused but unsettled",
			settleErr:    errors.New("acknowledgement lost"),
			wantReported: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var warnings bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(
				&warnings, &slog.HandlerOptions{Level: slog.LevelWarn},
			)))
			t.Cleanup(func() {
				slog.SetDefault(previous)
				reported := strings.Contains(warnings.String(), `"msg":"ingest batch ack failed"`)
				if reported != tc.wantReported {
					t.Errorf("settlement failure reported = %v, want %v", reported, tc.wantReported)
				}
			})
			settled := make(chan string, 1)
			delivery := settlingDelivery(notModifiedBatch("https://example.org/page"), settled)
			settle := func(context.Context) error {
				settled <- "settled"

				return tc.settleErr
			}
			delivery.Ack, delivery.AckRecordedNotModified = settle, settle

			newConditionalConsumerRun(&tc.conditional).absorb(t, delivery)

			if outcome := awaitSettlement(t, settled); outcome != "settled" {
				t.Fatalf("settlement = %q, want settled", outcome)
			}
		})
	}
}

func TestRecordedNotModifiedWithoutAConfirmationHookIsAcknowledged(t *testing.T) {
	run := newConditionalConsumerRun(&scriptedConditionalRecrawl{recorded: true})
	settled := make(chan string, 1)
	delivery := settlingDelivery(notModifiedBatch("https://example.org/page"), settled)
	delivery.AckRecordedNotModified = nil

	run.absorb(t, delivery)

	if outcome := awaitSettlement(t, settled); outcome != "ack" {
		t.Fatalf("settlement = %q, want ack", outcome)
	}
}

func TestNotModifiedObservationWithoutConditionalRecrawlIsAcknowledgedUnrecorded(t *testing.T) {
	run := newConditionalConsumerRun(nil)
	settled := make(chan string, 1)

	run.absorb(t, settlingDelivery(notModifiedBatch("https://example.org/page"), settled))

	if outcome := awaitSettlement(t, settled); outcome != "ack" {
		t.Fatalf("settlement = %q, want ack", outcome)
	}
}

func TestNotModifiedObservationIsNotRecordedOutsideItsLeaseOrProfile(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ownership crawlresults.OwnershipCheck
		lease     error
		want      string
	}{
		{name: "lost lease", lease: crawlresults.ErrLeaseLost, want: "lease lost"},
		{name: "unowned profile", ownership: fakeOwnership{}, want: "ack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conditional := &scriptedConditionalRecrawl{recorded: true}
			run := newConditionalConsumerRun(conditional)
			run.ownership = tc.ownership
			settled := make(chan string, 1)
			delivery := settlingDelivery(notModifiedBatch("https://example.org/page"), settled)
			delivery.AuthorizeLeaseSnapshot = func(context.Context) error { return tc.lease }

			run.absorb(t, delivery)

			if outcome := awaitSettlement(t, settled); outcome != tc.want {
				t.Fatalf("settlement = %q, want %q", outcome, tc.want)
			}
			if len(conditional.notModifiedURLs) != 0 {
				t.Fatalf("recorded %v outside the lease or profile", conditional.notModifiedURLs)
			}
		})
	}
}

func TestMicroBatchSettlesNotModifiedApartFromDocuments(t *testing.T) {
	conditional := &scriptedConditionalRecrawl{recorded: true}
	run := newConditionalConsumerRun(conditional)
	unchanged := make(chan string, 1)
	changed := make(chan string, 1)

	run.absorb(t,
		settlingDelivery(notModifiedBatch("https://example.org/unchanged"), unchanged),
		settlingDelivery(validatedDocumentBatch("https://example.org/changed"), changed),
	)

	if outcome := awaitSettlement(t, unchanged); outcome != "recorded" {
		t.Fatalf("not-modified settlement = %q, want recorded", outcome)
	}
	if outcome := awaitSettlement(t, changed); outcome != "ack" {
		t.Fatalf("document settlement = %q, want ack", outcome)
	}
	if len(conditional.validatedBatchURL) != 1 ||
		conditional.validatedBatchURL[0] != "https://example.org/changed" {
		t.Fatalf("validators recorded for %v, want only the stored document",
			conditional.validatedBatchURL)
	}
	if run.documents.calls != 1 {
		t.Fatalf("document store calls = %d, want 1", run.documents.calls)
	}
}

func TestResponseValidatorsAreRecordedOnlyForAStoredDocument(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pages       []string
		storeErr    error
		want        string
		recordedFor int
	}{
		{name: "single stored", pages: []string{"a"}, want: "ack", recordedFor: 1},
		{name: "group stored", pages: []string{"a", "b"}, want: "ack", recordedFor: 2},
		{name: "single failed", pages: []string{"a"}, storeErr: errors.New("disk full"), want: "nak"},
		{
			name:     "group failed",
			pages:    []string{"a", "b"},
			storeErr: errors.New("disk full"),
			want:     "nak",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conditional := &scriptedConditionalRecrawl{}
			run := newConditionalConsumerRun(conditional)
			run.documents.err = tc.storeErr
			settled := make(chan string, len(tc.pages))
			deliveries := make([]crawlresults.IngestDelivery, 0, len(tc.pages))
			for _, page := range tc.pages {
				batch := validatedDocumentBatch("https://example.org/" + page)
				deliveries = append(deliveries, settlingDelivery(batch, settled))
			}

			run.absorb(t, deliveries...)

			for range tc.pages {
				if outcome := awaitSettlement(t, settled); outcome != tc.want {
					t.Fatalf("settlement = %q, want %q", outcome, tc.want)
				}
			}
			if len(conditional.validatedBatchURL) != tc.recordedFor {
				t.Fatalf("validators recorded for %v, want %d pages",
					conditional.validatedBatchURL, tc.recordedFor)
			}
		})
	}
}

func TestResponseValidatorFailureDoesNotFailTheStoredDocument(t *testing.T) {
	conditional := &scriptedConditionalRecrawl{validatorsErr: errors.New("vault closed")}
	run := newConditionalConsumerRun(conditional)
	settled := make(chan string, 1)

	run.absorb(t, settlingDelivery(validatedDocumentBatch("https://example.org/page"), settled))

	if outcome := awaitSettlement(t, settled); outcome != "ack" {
		t.Fatalf("settlement = %q, want ack", outcome)
	}
	if len(conditional.validatedBatchURL) != 1 {
		t.Fatalf("validators recorded for %v, want the stored page", conditional.validatedBatchURL)
	}
}
