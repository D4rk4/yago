package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type failingFetchRecorder struct{ calls int }

func (r *failingFetchRecorder) RecordFetch(
	context.Context,
	string, string,
	time.Time,
) error {
	r.calls++

	return errors.New("record failed")
}

func fetchBatch() yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/page",
		ProfileHandle: "handle-1",
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org/page",
			ExtractedText: "body",
			FetchedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
		},
	}
}

// runUntilSettled runs the consumer until the delivery's Ack/Nak fires (wg), then
// cancels; the deferred cancel runs after wg.Wait so the delivery is processed.
func runUntilSettled(consumer *crawlresults.IngestConsumer, wg *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()
}

// TestRecordFetchDefaultRecorderIsNoop exercises the noop recorder installed when
// RecordFetches is never called: an absorbed page batch still records its fetch.
func TestRecordFetchDefaultRecorderIsNoop(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: fetchBatch(),
		Ack:   func(context.Context) error { wg.Done(); return nil },
		Nak:   func(context.Context) error { wg.Done(); return nil },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	runUntilSettled(consumer, &wg)
}

func TestRecordFetchLogsRecorderFailure(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: fetchBatch(),
		Ack:   func(context.Context) error { wg.Done(); return nil },
		Nak:   func(context.Context) error { wg.Done(); return nil },
	}
	recorder := &failingFetchRecorder{}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	consumer.RecordFetches(recorder)
	runUntilSettled(consumer, &wg)

	if recorder.calls != 1 {
		t.Fatalf("recorder calls = %d, want 1", recorder.calls)
	}
}

func TestRejectLogsAckFailure(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{}, // no source URL: rejected as malformed
		Ack:   func(context.Context) error { defer wg.Done(); return errors.New("ack failed") },
		Nak:   func(context.Context) error { defer wg.Done(); return nil },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	runUntilSettled(consumer, &wg)
}

// A schedule write failure leaves the page stored and searchable but without a
// durable recrawl entry, so it is counted as well as logged: an operator can
// alert on the gap instead of grepping for it.
func TestRecordFetchCountsScheduleFailures(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: fetchBatch(),
		Ack:   func(context.Context) error { wg.Done(); return nil },
		Nak:   func(context.Context) error { wg.Done(); return nil },
	}
	observer := &recordingObserver{}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	consumer.Observe(observer)
	consumer.RecordFetches(&failingFetchRecorder{})
	runUntilSettled(consumer, &wg)

	if observer.scheduleFailures != 1 {
		t.Fatalf("schedule failures = %d, want 1", observer.scheduleFailures)
	}
}

// A dead-page tombstone whose acknowledgement fails is logged and dropped: the
// purge already happened and is idempotent, so a redelivery would repeat it.
func TestPurgeRemovalLogsAckFailure(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://gone.example/page",
			Removed:   true,
		},
		Ack: func(context.Context) error { defer wg.Done(); return errors.New("ack failed") },
		Nak: func(context.Context) error { defer wg.Done(); return nil },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	runUntilSettled(consumer, &wg)
}
