package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/crawlresults"
	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
)

type fakeStream struct {
	out chan crawlresults.IngestDelivery
}

func (s *fakeStream) Receive() <-chan crawlresults.IngestDelivery { return s.out }

type recordingDocumentReceiver struct {
	receipt documentstore.Receipt
	err     error
	calls   int
	at      time.Time
}

func (r *recordingDocumentReceiver) Receive(
	_ context.Context,
	_ []documentstore.Document,
) (documentstore.Receipt, error) {
	r.calls++
	r.at = time.Now()
	return r.receipt, r.err
}

type recordingURLReceiver struct {
	receipt urlmeta.Receipt
	err     error
	calls   int
	at      time.Time
}

func (r *recordingURLReceiver) Receive(
	_ context.Context,
	_ []yacymodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	r.calls++
	r.at = time.Now()
	return r.receipt, r.err
}

type recordingPostingReceiver struct {
	receipt rwi.Receipt
	err     error
	calls   int
	at      time.Time
}

func (r *recordingPostingReceiver) Receive(
	_ context.Context,
	_ []yacymodel.RWIPosting,
) (rwi.Receipt, error) {
	r.calls++
	r.at = time.Now()
	return r.receipt, r.err
}

type deliveryCallbacks struct {
	ack func(context.Context) error
	nak func(context.Context) error
}

func deliver(
	t *testing.T,
	documents *recordingDocumentReceiver,
	urls *recordingURLReceiver,
	postings *recordingPostingReceiver,
) (acked, naked bool) {
	t.Helper()
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yacycrawlcontract.IngestBatch{
			SourceURL: "https://example.org",
			Document: yacycrawlcontract.DocumentIngest{
				NormalizedURL: "https://example.org",
				ExtractedText: "body",
				Inlinks: []yacycrawlcontract.AnchorText{
					{URL: "https://source.example/", Text: "source"},
				},
			},
		},
		Ack: func(context.Context) error { acked = true; wg.Done(); return nil },
		Nak: func(context.Context) error { naked = true; wg.Done(); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumer := crawlresults.NewIngestConsumer(stream, documents, urls, postings)
	go consumer.Run(ctx)
	wg.Wait()
	return acked, naked
}

func deliverWithCallbacks(
	t *testing.T,
	documents *recordingDocumentReceiver,
	urls *recordingURLReceiver,
	postings *recordingPostingReceiver,
	callbacks deliveryCallbacks,
) {
	t.Helper()
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yacycrawlcontract.IngestBatch{
			SourceURL: "https://example.org",
			Document: yacycrawlcontract.DocumentIngest{
				NormalizedURL: "https://example.org",
				ExtractedText: "body",
			},
		},
		Ack: func(ctx context.Context) error {
			defer wg.Done()
			return callbacks.ack(ctx)
		},
		Nak: func(ctx context.Context) error {
			defer wg.Done()
			return callbacks.nak(ctx)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumer := crawlresults.NewIngestConsumer(stream, documents, urls, postings)
	go consumer.Run(ctx)
	wg.Wait()
}

func TestAbsorbStoresMetadataBeforePostingsAndAcks(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, documents, urls, postings)

	if !acked || naked {
		t.Fatalf("acked=%v naked=%v, want acked", acked, naked)
	}
	if documents.calls != 1 || urls.calls != 1 || postings.calls != 1 {
		t.Fatalf(
			"documents.calls=%d urls.calls=%d postings.calls=%d, want 1/1/1",
			documents.calls,
			urls.calls,
			postings.calls,
		)
	}
	if !documents.at.Before(urls.at) || !urls.at.Before(postings.at) {
		t.Fatal("documents, metadata, and postings must be stored in order")
	}
}

func TestAbsorbLogsAckFailure(t *testing.T) {
	deliverWithCallbacks(
		t,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
		deliveryCallbacks{
			ack: func(context.Context) error { return errors.New("ack failed") },
			nak: func(context.Context) error {
				t.Fatal("unexpected nak")
				return nil
			},
		},
	)
}

func TestAbsorbNaksWhenURLReceiverBusy(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{receipt: urlmeta.Receipt{Busy: true}}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, documents, urls, postings)

	if acked || !naked {
		t.Fatalf("acked=%v naked=%v, want naked", acked, naked)
	}
	if postings.calls != 0 {
		t.Fatal("postings must not be stored when url receiver is busy")
	}
}

func TestAbsorbNaksWhenDocumentReceiverBusy(t *testing.T) {
	documents := &recordingDocumentReceiver{receipt: documentstore.Receipt{Busy: true}}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, documents, urls, postings)

	if acked || !naked {
		t.Fatalf("acked=%v naked=%v, want naked", acked, naked)
	}
	if urls.calls != 0 || postings.calls != 0 {
		t.Fatalf("urls.calls=%d postings.calls=%d, want 0/0", urls.calls, postings.calls)
	}
}

func TestAbsorbNaksWhenPostingReceiverErrors(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{err: errors.New("boom")}
	acked, naked := deliver(t, documents, urls, postings)

	if acked || !naked {
		t.Fatalf("acked=%v naked=%v, want naked", acked, naked)
	}
}

func TestAbsorbLogsNakFailure(t *testing.T) {
	deliverWithCallbacks(
		t,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{err: errors.New("url failed")},
		&recordingPostingReceiver{},
		deliveryCallbacks{
			ack: func(context.Context) error {
				t.Fatal("unexpected ack")
				return nil
			},
			nak: func(context.Context) error { return errors.New("nak failed") },
		},
	)
}

func TestRunStopsWhenStreamCloses(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery)}
	close(stream.out)
	done := make(chan struct{})
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	go func() { consumer.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return when stream closed")
	}
}

func TestRunStopsWhenContextEnds(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	go func() { consumer.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return when context ended")
	}
}
