package crawlresults_test

import (
	"context"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// batchRecordingReceiver counts Receive calls and the documents each carried.
type batchRecordingReceiver struct {
	mu     sync.Mutex
	calls  int
	sizes  []int
	busy   bool
	docErr error
}

func (r *batchRecordingReceiver) Receive(
	_ context.Context,
	docs []documentstore.Document,
) (documentstore.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.sizes = append(r.sizes, len(docs))
	if r.docErr != nil {
		return documentstore.Receipt{}, r.docErr
	}

	return documentstore.Receipt{Busy: r.busy}, nil
}

// batchRecordingIndex offers the bulk path and records how it was used.
type batchRecordingIndex struct {
	recordingSearchIndex

	mu         sync.Mutex
	batchCalls int
	batchSizes []int
}

func (i *batchRecordingIndex) IndexBatch(
	_ context.Context,
	docs []documentstore.Document,
) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.batchCalls++
	i.batchSizes = append(i.batchSizes, len(docs))

	return nil
}

func microBatchPage(url string, acked *sync.WaitGroup) crawlresults.IngestDelivery {
	acked.Add(1)

	return crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: url,
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: url,
				ExtractedText: "содержимое страницы " + url,
			},
		},
		Ack: func(context.Context) error { acked.Done(); return nil },
		Nak: func(context.Context) error { acked.Done(); return nil },
	}
}

// TestIngestGroupsPendingDeliveriesIntoOneStoreAndIndexBatch is the PERF-05
// acceptance: deliveries already waiting on the stream land in the vault
// through one Receive and in the index through one bulk batch.
func TestIngestGroupsPendingDeliveriesIntoOneStoreAndIndexBatch(t *testing.T) {
	documents := &batchRecordingReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	index := &batchRecordingIndex{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 8)}
	consumer := crawlresults.NewIngestConsumerWithIndex(stream, documents, index, urls, postings)
	consumer.Observe(&recordingObserver{})

	var acked sync.WaitGroup
	for _, url := range []string{
		"https://example.org/a",
		"https://example.org/b",
		"https://example.org/c",
	} {
		stream.out <- microBatchPage(url, &acked)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	acked.Wait()

	documents.mu.Lock()
	defer documents.mu.Unlock()
	if documents.calls != 1 || documents.sizes[0] != 3 {
		t.Fatalf("vault receives = %d sizes=%v, want one call carrying 3 docs",
			documents.calls, documents.sizes)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.batchCalls != 1 || index.batchSizes[0] != 3 {
		t.Fatalf("index batches = %d sizes=%v, want one bulk batch of 3",
			index.batchCalls, index.batchSizes)
	}
	if index.calls != 0 {
		t.Fatalf("per-document Index called %d times despite the bulk path",
			index.calls)
	}
}

// TestIngestGroupFallsBackToPerDocumentIndexing covers indexes without a bulk
// path: the group still shares one vault Receive while indexing per document.
func TestIngestGroupFallsBackToPerDocumentIndexing(t *testing.T) {
	documents := &batchRecordingReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	index := &recordingSearchIndex{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumerWithIndex(stream, documents, index, urls, postings)
	consumer.Observe(&recordingObserver{})

	var acked sync.WaitGroup
	stream.out <- microBatchPage("https://example.org/a", &acked)
	stream.out <- microBatchPage("https://example.org/b", &acked)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	acked.Wait()

	documents.mu.Lock()
	defer documents.mu.Unlock()
	if documents.calls != 1 || documents.sizes[0] != 2 {
		t.Fatalf("vault receives = %d sizes=%v", documents.calls, documents.sizes)
	}
	if index.calls != 2 {
		t.Fatalf("per-document Index calls = %d, want 2", index.calls)
	}
}

// TestIngestGroupRedeliversAllCarriersOnStoreBackpressure: when the grouped
// vault write reports capacity, every document-carrying delivery goes back to
// the stream (Nak) and none is acked as absorbed.
func TestIngestGroupRedeliversAllCarriersOnStoreBackpressure(t *testing.T) {
	documents := &batchRecordingReceiver{busy: true}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	index := &batchRecordingIndex{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumerWithIndex(stream, documents, index, urls, postings)
	consumer.Observe(&recordingObserver{})

	var settled sync.WaitGroup
	naks := 0
	var mu sync.Mutex
	deliver := func(url string) {
		settled.Add(1)
		stream.out <- crawlresults.IngestDelivery{
			Batch: yagocrawlcontract.IngestBatch{
				SourceURL: url,
				Document: yagocrawlcontract.DocumentIngest{
					NormalizedURL: url,
					ExtractedText: "содержимое " + url,
				},
			},
			Ack: func(context.Context) error { settled.Done(); return nil },
			Nak: func(context.Context) error {
				mu.Lock()
				naks++
				mu.Unlock()
				settled.Done()

				return nil
			},
		}
	}
	deliver("https://example.org/a")
	deliver("https://example.org/b")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	settled.Wait()

	mu.Lock()
	defer mu.Unlock()
	if naks != 2 {
		t.Fatalf("naks = %d, want both carriers redelivered", naks)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.batchCalls != 0 {
		t.Fatal("index must not run when the store reported capacity")
	}
}
