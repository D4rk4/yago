package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawlresults"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
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
	segment yacycrawlcontract.CrawledPageIndexSegment,
	urls *recordingURLReceiver,
	postings *recordingPostingReceiver,
) (acked, naked bool) {
	t.Helper()
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Segment: segment,
		Ack:     func(context.Context) error { acked = true; wg.Done(); return nil },
		Nak:     func(context.Context) error { naked = true; wg.Done(); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumer := crawlresults.NewIngestConsumer(stream, urls, postings)
	go consumer.Run(ctx)
	wg.Wait()
	return acked, naked
}

func metadataSegment() yacycrawlcontract.CrawledPageIndexSegment {
	return yacycrawlcontract.CrawledPageIndexSegment{
		CanonicalURL: "https://example.org",
		Metadata: []yacymodel.URIMetadataRow{
			{Properties: map[string]string{"u": "urlhash01234"}},
		},
	}
}

func postingsSegment() yacycrawlcontract.CrawledPageIndexSegment {
	return yacycrawlcontract.CrawledPageIndexSegment{
		CanonicalURL: "https://example.org",
		Postings:     []yacymodel.RWIPosting{{WordHash: yacymodel.WordHash("w")}},
	}
}

func TestAbsorbMetadataSegmentStoresURLsAndAcks(t *testing.T) {
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, metadataSegment(), urls, postings)

	if !acked || naked {
		t.Fatalf("acked=%v naked=%v, want acked", acked, naked)
	}
	if urls.calls != 1 || postings.calls != 0 {
		t.Fatalf("urls.calls=%d postings.calls=%d, want 1/0", urls.calls, postings.calls)
	}
}

func TestAbsorbPostingsSegmentStoresPostingsAndAcks(t *testing.T) {
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, postingsSegment(), urls, postings)

	if !acked || naked {
		t.Fatalf("acked=%v naked=%v, want acked", acked, naked)
	}
	if urls.calls != 0 || postings.calls != 1 {
		t.Fatalf("urls.calls=%d postings.calls=%d, want 0/1", urls.calls, postings.calls)
	}
}

func TestAbsorbNaksWhenURLReceiverBusy(t *testing.T) {
	urls := &recordingURLReceiver{receipt: urlmeta.Receipt{Busy: true}}
	postings := &recordingPostingReceiver{}
	acked, naked := deliver(t, metadataSegment(), urls, postings)

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
	acked, naked := deliver(t, postingsSegment(), urls, postings)

	if acked || !naked {
		t.Fatalf("acked=%v naked=%v, want naked", acked, naked)
	}
}

func TestAbsorbNaksWhenPostingBatchTooLarge(t *testing.T) {
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{receipt: rwi.Receipt{Busy: true, TooLarge: true}}
	acked, naked := deliver(t, postingsSegment(), urls, postings)

	if acked || !naked {
		t.Fatalf("acked=%v naked=%v, want naked", acked, naked)
	}
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
