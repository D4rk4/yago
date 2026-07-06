package crawlresults_test

import (
	"context"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type recordingObserver struct {
	absorbed   int
	deferred   int
	rejected   int
	duplicates int
	lowQuality int
	bytes      int
	urls       int
	postings   int
}

func (o *recordingObserver) ObserveAbsorbed(bytes, urls, postings int) {
	o.absorbed++
	o.bytes += bytes
	o.urls += urls
	o.postings += postings
}

func (o *recordingObserver) ObserveDuplicate() {
	o.duplicates++
}

func (o *recordingObserver) ObserveLowQuality() {
	o.lowQuality++
}

func (o *recordingObserver) ObserveDeferred() { o.deferred++ }

func (o *recordingObserver) ObserveRejected() { o.rejected++ }

func runObserved(
	t *testing.T,
	urls *recordingURLReceiver,
	batch yagocrawlcontract.IngestBatch,
) *recordingObserver {
	t.Helper()

	var wg sync.WaitGroup
	wg.Add(1)
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	stream.out <- crawlresults.IngestDelivery{
		Batch: batch,
		Ack:   func(context.Context) error { wg.Done(); return nil },
		Nak:   func(context.Context) error { wg.Done(); return nil },
	}
	observer := &recordingObserver{}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		urls,
		&recordingPostingReceiver{},
	)
	consumer.Observe(observer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()

	return observer
}

func TestObserverCountsAbsorbedBatch(t *testing.T) {
	observer := runObserved(t, &recordingURLReceiver{}, yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org",
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org",
			ExtractedText: "body",
		},
		Metadata: []yagomodel.URIMetadataRow{{}},
		Postings: []yagomodel.RWIPosting{{}, {}},
	})

	if observer.absorbed != 1 || observer.deferred != 0 {
		t.Fatalf("absorbed=%d deferred=%d", observer.absorbed, observer.deferred)
	}
	if observer.bytes != len("body") || observer.urls != 1 || observer.postings != 2 {
		t.Fatalf(
			"bytes=%d urls=%d postings=%d",
			observer.bytes, observer.urls, observer.postings,
		)
	}
}

func TestObserverCountsDeferredBatch(t *testing.T) {
	observer := runObserved(t, &recordingURLReceiver{receipt: urlmeta.Receipt{Busy: true}},
		yagocrawlcontract.IngestBatch{
			SourceURL: "https://example.org",
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: "https://example.org",
				ExtractedText: "body",
			},
		})

	if observer.deferred != 1 || observer.absorbed != 0 {
		t.Fatalf("deferred=%d absorbed=%d", observer.deferred, observer.absorbed)
	}
}

func TestObserverCountsRejectedBatch(t *testing.T) {
	urls := &recordingURLReceiver{}
	observer := runObserved(t, urls, yagocrawlcontract.IngestBatch{
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org",
			ExtractedText: "body",
		},
	})

	if observer.rejected != 1 || observer.absorbed != 0 || observer.deferred != 0 {
		t.Fatalf("rejected=%d absorbed=%d deferred=%d",
			observer.rejected, observer.absorbed, observer.deferred)
	}
	if urls.calls != 0 {
		t.Fatalf("url receiver called %d times for rejected batch, want 0", urls.calls)
	}
}

func TestObserverRejectsDocumentWithoutURL(t *testing.T) {
	observer := runObserved(t, &recordingURLReceiver{}, yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org",
		Document: yagocrawlcontract.DocumentIngest{
			ExtractedText: "body",
		},
	})

	if observer.rejected != 1 || observer.absorbed != 0 {
		t.Fatalf("rejected=%d absorbed=%d", observer.rejected, observer.absorbed)
	}
}

func TestRejectedBatchIsAckedNotNaked(t *testing.T) {
	var acked, naked bool
	var wg sync.WaitGroup
	wg.Add(1)
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{},
		Ack:   func(context.Context) error { acked = true; wg.Done(); return nil },
		Nak:   func(context.Context) error { naked = true; wg.Done(); return nil },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()

	if !acked || naked {
		t.Fatalf("malformed batch acked=%v naked=%v, want dropped (ack, no nak)", acked, naked)
	}
}
