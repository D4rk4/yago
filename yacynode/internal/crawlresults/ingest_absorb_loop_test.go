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
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
)

type fakeStream struct {
	out chan crawlresults.IngestDelivery
}

func (s *fakeStream) Receive() <-chan crawlresults.IngestDelivery { return s.out }

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

func deliver(
	t *testing.T,
	urls *recordingURLReceiver,
	postings *recordingPostingReceiver,
) (acked, naked bool) {
	t.Helper()
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yacycrawlcontract.IngestBatch{SourceURL: "https://example.org"},
		Ack:   func(context.Context) error { acked = true; wg.Done(); return nil },
		Nak:   func(context.Context) error { naked = true; wg.Done(); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumer := crawlresults.NewIngestConsumer(stream, urls, postings)
	go consumer.Run(ctx)
	wg.Wait()
	return acked, naked
}

func deliverWithCallbacks(
	t *testing.T,
	urls *recordingURLReceiver,
	postings *recordingPostingReceiver,
	ack func(context.Context) error,
	nak func(context.Context) error,
) {
	t.Helper()
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yacycrawlcontract.IngestBatch{SourceURL: "https://example.org"},
		Ack: func(ctx context.Context) error {
			defer wg.Done()
			return ack(ctx)
		},
		Nak: func(ctx context.Context) error {
			defer wg.Done()
			return nak(ctx)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumer := crawlresults.NewIngestConsumer(stream, urls, postings)
	go consumer.Run(ctx)
	wg.Wait()
}

func TestAbsorbStoresMetadataBeforePostingsAndAcks(t *testing.T) {
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, urls, postings)

	if !acked || naked {
		t.Fatalf("acked=%v naked=%v, want acked", acked, naked)
	}
	if urls.calls != 1 || postings.calls != 1 {
		t.Fatalf("urls.calls=%d postings.calls=%d, want 1/1", urls.calls, postings.calls)
	}
	if !urls.at.Before(postings.at) {
		t.Fatal("metadata must be stored before postings")
	}
}

func TestAbsorbLogsAckFailure(t *testing.T) {
	deliverWithCallbacks(
		t,
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
		func(context.Context) error { return errors.New("ack failed") },
		func(context.Context) error {
			t.Fatal("unexpected nak")
			return nil
		},
	)
}

func TestAbsorbNaksWhenURLReceiverBusy(t *testing.T) {
	urls := &recordingURLReceiver{receipt: urlmeta.Receipt{Busy: true}}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, urls, postings)

	if acked || !naked {
		t.Fatalf("acked=%v naked=%v, want naked", acked, naked)
	}
	if postings.calls != 0 {
		t.Fatal("postings must not be stored when url receiver is busy")
	}
}

func TestAbsorbNaksWhenPostingReceiverErrors(t *testing.T) {
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{err: errors.New("boom")}
	acked, naked := deliver(t, urls, postings)

	if acked || !naked {
		t.Fatalf("acked=%v naked=%v, want naked", acked, naked)
	}
}

func TestAbsorbLogsNakFailure(t *testing.T) {
	deliverWithCallbacks(
		t,
		&recordingURLReceiver{err: errors.New("url failed")},
		&recordingPostingReceiver{},
		func(context.Context) error {
			t.Fatal("unexpected ack")
			return nil
		},
		func(context.Context) error { return errors.New("nak failed") },
	)
}

func TestRunStopsWhenStreamCloses(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery)}
	close(stream.out)
	done := make(chan struct{})
	consumer := crawlresults.NewIngestConsumer(
		stream,
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
