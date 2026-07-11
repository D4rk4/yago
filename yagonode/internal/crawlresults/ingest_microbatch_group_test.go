package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

const clusteredPageText = "routing platform hypervisor interface configuration " +
	"tunnel policy route firmware console command log debugging monitoring"

// settleCounter tallies how a micro-batch's deliveries were acknowledged.
type settleCounter struct {
	mu   sync.Mutex
	acks int
	naks int
}

func (c *settleCounter) counts() (acks, naks int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.acks, c.naks
}

// groupDelivery wires a batch onto a WaitGroup and the shared counter; ackErr is
// the (usually nil) error the Ack returns so the ack-failure branch is reachable.
func groupDelivery(
	batch yagocrawlcontract.IngestBatch,
	wg *sync.WaitGroup,
	counter *settleCounter,
	ackErr error,
) crawlresults.IngestDelivery {
	wg.Add(1)

	return crawlresults.IngestDelivery{
		Batch: batch,
		Ack: func(context.Context) error {
			counter.mu.Lock()
			counter.acks++
			counter.mu.Unlock()
			wg.Done()

			return ackErr
		},
		Nak: func(context.Context) error {
			counter.mu.Lock()
			counter.naks++
			counter.mu.Unlock()
			wg.Done()

			return nil
		},
	}
}

// drainGroup runs the consumer until every pre-buffered delivery has settled.
func drainGroup(consumer *crawlresults.IngestConsumer, wg *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()
}

func metaOnlyBatch(url string) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{SourceURL: url}
}

func postingsBatch(url string) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL: url,
		Postings:  []yagomodel.RWIPosting{{WordHash: yagomodel.WordHash(url)}},
	}
}

func groupDocBatch(url, text string) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL: url,
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL:               url,
			ExtractedText:               text,
			OutboundAnchorEvidenceKnown: true,
		},
	}
}

func fetchDocBatch(url string) yagocrawlcontract.IngestBatch {
	batch := groupDocBatch(url, "текст "+url)
	batch.Document.FetchedAt = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

	return batch
}

// batchGroupSweeper offers the whole-batch sweep so a group sweeps in one call.
type batchGroupSweeper struct {
	mu         sync.Mutex
	batchCalls int
	batchErr   error
}

func (s *batchGroupSweeper) PurgeStalePostings(
	context.Context,
	yagomodel.Hash,
	map[yagomodel.Hash]struct{},
) (int, error) {
	return 0, nil
}

func (s *batchGroupSweeper) PurgeStalePostingsForURLs(
	_ context.Context,
	staleByURL map[yagomodel.Hash]map[yagomodel.Hash]struct{},
) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++

	return len(staleByURL), s.batchErr
}

func (s *batchGroupSweeper) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.batchCalls
}

// plainGroupSweeper offers only the per-URL sweep, so a group falls back to the
// per-delivery path.
type plainGroupSweeper struct {
	err error
}

func (s *plainGroupSweeper) PurgeStalePostings(
	context.Context,
	yagomodel.Hash,
	map[yagomodel.Hash]struct{},
) (int, error) {
	return 0, s.err
}

// batchGroupRecorder offers the whole-batch recrawl record.
type batchGroupRecorder struct {
	mu         sync.Mutex
	batchCalls int
	batchErr   error
}

func (r *batchGroupRecorder) RecordFetch(context.Context, string, string, time.Time) error {
	return nil
}

func (r *batchGroupRecorder) RecordFetches(
	context.Context,
	[]string,
	[]string,
	[]time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batchCalls++

	return r.batchErr
}

func (r *batchGroupRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.batchCalls
}

func TestGroupStoresEveryDocumentAndSendsMetadataOnlyToTail(t *testing.T) {
	documents := &batchRecordingReceiver{}
	index := &batchRecordingIndex{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumerWithIndex(
		stream, documents, index, &recordingURLReceiver{}, &recordingPostingReceiver{},
	)

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(metaOnlyBatch("https://example.org/meta"), &wg, counter, nil)
	stream.out <- groupDelivery(
		groupDocBatch("https://example.org/a", clusteredPageText),
		&wg,
		counter,
		nil,
	)
	stream.out <- groupDelivery(
		groupDocBatch("https://example.org/b", clusteredPageText),
		&wg,
		counter,
		nil,
	)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 3 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want 3/0", acks, naks)
	}
	documents.mu.Lock()
	defer documents.mu.Unlock()
	if documents.calls != 1 || documents.sizes[0] != 2 {
		t.Fatalf("stored sizes = %v, want one call carrying both documents", documents.sizes)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.batchCalls != 1 || index.batchSizes[0] != 2 {
		t.Fatalf("index sizes = %v, want one batch carrying both documents", index.batchSizes)
	}
}

// TestGroupWithOneRejectedDeliveryTakesSingleTailPath covers absorbTailGroup's
// single-survivor branch: when a gate drops all but one of a group, the lone
// survivor takes the plain per-delivery tail.
func TestGroupWithOneRejectedDeliveryTakesSingleTailPath(t *testing.T) {
	urls := &recordingURLReceiver{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream, &recordingDocumentReceiver{}, urls, &recordingPostingReceiver{},
	)
	consumer.CheckOwnership(fakeOwnership{owns: map[string]bool{"keep": true}})

	owned := metaOnlyBatch("https://example.org/keep")
	owned.ProfileHandle = "keep"
	foreign := metaOnlyBatch("https://example.org/drop")
	foreign.ProfileHandle = "foreign"

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(owned, &wg, counter, nil)
	stream.out <- groupDelivery(foreign, &wg, counter, nil)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 2 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want 2/0 (owned absorbed, foreign rejected-acked)", acks, naks)
	}
	if urls.calls != 1 {
		t.Fatalf("url receives = %d, want 1 (the single surviving tail)", urls.calls)
	}
}

// TestGroupRedeliversWholeGroupOnTailStoreFault covers absorbTailGroup's four
// redelivery exits: a fault or capacity signal from the URL or posting store
// naks the whole group.
func TestGroupRedeliversWholeGroupOnTailStoreFault(t *testing.T) {
	boom := errors.New("store fault")
	cases := []struct {
		name     string
		urls     *recordingURLReceiver
		postings *recordingPostingReceiver
	}{
		{"url error", &recordingURLReceiver{err: boom}, &recordingPostingReceiver{}},
		{
			"url busy",
			&recordingURLReceiver{receipt: urlmeta.Receipt{Busy: true}},
			&recordingPostingReceiver{},
		},
		{"posting error", &recordingURLReceiver{}, &recordingPostingReceiver{err: boom}},
		{
			"posting busy",
			&recordingURLReceiver{},
			&recordingPostingReceiver{receipt: rwi.Receipt{Busy: true}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
			consumer := crawlresults.NewIngestConsumer(
				stream, &recordingDocumentReceiver{}, tc.urls, tc.postings,
			)
			var wg sync.WaitGroup
			counter := &settleCounter{}
			stream.out <- groupDelivery(metaOnlyBatch("https://example.org/a"), &wg, counter, nil)
			stream.out <- groupDelivery(metaOnlyBatch("https://example.org/b"), &wg, counter, nil)
			drainGroup(consumer, &wg)

			if _, naks := counter.counts(); naks != 2 {
				t.Fatalf("naks = %d, want both redelivered", naks)
			}
		})
	}
}

// TestGroupBatchSweepFailureRedeliversAndEmptiesGroup covers the grouped stale
// sweep's failure exit and absorbTailGroup's empty-after-sweep return.
func TestGroupBatchSweepFailureRedeliversAndEmptiesGroup(t *testing.T) {
	postings := &recordingPostingReceiver{}
	sweeper := &batchGroupSweeper{batchErr: errors.New("sweep down")}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream, &recordingDocumentReceiver{}, &recordingURLReceiver{}, postings,
	)
	consumer.SweepStalePostings(sweeper)

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(postingsBatch("https://example.org/a"), &wg, counter, nil)
	stream.out <- groupDelivery(postingsBatch("https://example.org/b"), &wg, counter, nil)
	drainGroup(consumer, &wg)

	if _, naks := counter.counts(); naks != 2 {
		t.Fatalf("naks = %d, want both redelivered after the batch sweep failed", naks)
	}
	if postings.calls != 0 {
		t.Fatal("postings must not be stored after the sweep emptied the group")
	}
}

// TestGroupFallbackSweepFailureRedeliversPerDelivery covers the per-delivery
// sweep fallback taken when the sweeper offers no whole-batch method.
func TestGroupFallbackSweepFailureRedeliversPerDelivery(t *testing.T) {
	postings := &recordingPostingReceiver{}
	sweeper := &plainGroupSweeper{err: errors.New("sweep down")}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream, &recordingDocumentReceiver{}, &recordingURLReceiver{}, postings,
	)
	consumer.SweepStalePostings(sweeper)

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(postingsBatch("https://example.org/a"), &wg, counter, nil)
	stream.out <- groupDelivery(postingsBatch("https://example.org/b"), &wg, counter, nil)
	drainGroup(consumer, &wg)

	if _, naks := counter.counts(); naks != 2 {
		t.Fatalf("naks = %d, want both redelivered by the per-delivery sweep", naks)
	}
	if postings.calls != 0 {
		t.Fatal("postings must not be stored after every delivery failed its sweep")
	}
}

// TestGroupBatchSweepSkipsPostingFreeDeliveryAndSucceeds covers the grouped
// sweep's happy path: a posting-free delivery is kept without sweeping while the
// posting-bearing one is swept, and the surviving group is absorbed.
func TestGroupBatchSweepSkipsPostingFreeDeliveryAndSucceeds(t *testing.T) {
	sweeper := &batchGroupSweeper{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream, &recordingDocumentReceiver{}, &recordingURLReceiver{}, &recordingPostingReceiver{},
	)
	consumer.SweepStalePostings(sweeper)

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(postingsBatch("https://example.org/a"), &wg, counter, nil)
	stream.out <- groupDelivery(metaOnlyBatch("https://example.org/b"), &wg, counter, nil)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 2 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want 2/0", acks, naks)
	}
	if sweeper.calls() != 1 {
		t.Fatalf("batch sweep calls = %d, want 1", sweeper.calls())
	}
}

// TestGroupBatchSweepAndRecordReturnEarlyWhenNothingApplies covers the grouped
// sweep's empty-set return and the grouped recorder's no-fetch return: a group
// of metadata-only deliveries neither sweeps nor records yet is fully absorbed.
func TestGroupBatchSweepAndRecordReturnEarlyWhenNothingApplies(t *testing.T) {
	sweeper := &batchGroupSweeper{}
	recorder := &batchGroupRecorder{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream, &recordingDocumentReceiver{}, &recordingURLReceiver{}, &recordingPostingReceiver{},
	)
	consumer.SweepStalePostings(sweeper)
	consumer.RecordFetches(recorder)

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(metaOnlyBatch("https://example.org/a"), &wg, counter, nil)
	stream.out <- groupDelivery(metaOnlyBatch("https://example.org/b"), &wg, counter, nil)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 2 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want 2/0", acks, naks)
	}
	if sweeper.calls() != 0 || recorder.calls() != 0 {
		t.Fatalf("sweep=%d record=%d, want 0/0 (nothing to sweep or record)",
			sweeper.calls(), recorder.calls())
	}
}

// TestGroupToleratesAckFailureWhenFinishingAbsorbed covers finishAbsorbed's
// ack-failure branch: one delivery's Ack fails yet the group still commits its
// grouped tail stores.
func TestGroupToleratesAckFailureWhenFinishingAbsorbed(t *testing.T) {
	urls := &recordingURLReceiver{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream, &recordingDocumentReceiver{}, urls, &recordingPostingReceiver{},
	)

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(
		metaOnlyBatch("https://example.org/a"), &wg, counter, errors.New("ack down"),
	)
	stream.out <- groupDelivery(metaOnlyBatch("https://example.org/b"), &wg, counter, nil)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 2 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want both finished despite an ack failure", acks, naks)
	}
	if urls.calls != 1 {
		t.Fatalf("url receives = %d, want 1 grouped call", urls.calls)
	}
}

// TestGroupRecordsFetchesInOneBatchAndToleratesFailure covers the grouped
// recorder: real fetches are batched and skips ignored, and a record failure is
// best-effort. The nil index also exercises storeDocumentGroup's no-index exit.
func TestGroupRecordsFetchesInOneBatchAndToleratesFailure(t *testing.T) {
	recorder := &batchGroupRecorder{batchErr: errors.New("recrawl down")}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
	consumer := crawlresults.NewIngestConsumer(
		stream, &batchRecordingReceiver{}, &recordingURLReceiver{}, &recordingPostingReceiver{},
	)
	consumer.RecordFetches(recorder)

	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(fetchDocBatch("https://example.org/a"), &wg, counter, nil)
	stream.out <- groupDelivery(metaOnlyBatch("https://example.org/b"), &wg, counter, nil)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 2 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want both absorbed despite the record failure", acks, naks)
	}
	if recorder.calls() != 1 {
		t.Fatalf("batch record calls = %d, want 1", recorder.calls())
	}
}

// TestGroupRedeliversDocumentCarriersOnStoreOrIndexFault covers
// storeDocumentGroup's document-store and search-index failure exits, including
// indexDocuments' per-document error return.
func TestGroupRedeliversDocumentCarriersOnStoreOrIndexFault(t *testing.T) {
	boom := errors.New("doc fault")
	cases := []struct {
		name      string
		documents *batchRecordingReceiver
		index     searchindex.SearchIndex
	}{
		{"document store error", &batchRecordingReceiver{docErr: boom}, &batchRecordingIndex{}},
		{"index error", &batchRecordingReceiver{}, &recordingSearchIndex{err: boom}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urls := &recordingURLReceiver{}
			stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 4)}
			consumer := crawlresults.NewIngestConsumerWithIndex(
				stream, tc.documents, tc.index, urls, &recordingPostingReceiver{},
			)
			var wg sync.WaitGroup
			counter := &settleCounter{}
			stream.out <- groupDelivery(groupDocBatch("https://example.org/a", "текст a"), &wg, counter, nil)
			stream.out <- groupDelivery(groupDocBatch("https://example.org/b", "текст b"), &wg, counter, nil)
			drainGroup(consumer, &wg)

			if _, naks := counter.counts(); naks != 2 {
				t.Fatalf("naks = %d, want both carriers redelivered", naks)
			}
			if urls.calls != 0 {
				t.Fatalf(
					"url receives = %d, want 0 (tail must not run after a doc fault)",
					urls.calls,
				)
			}
		})
	}
}

// TestSingleDeliveryRedeliversOnTailFault covers the single-delivery absorb
// path's redelivery exits: a document-store fault, a stale-sweep fault, and
// posting-store capacity each nak the lone delivery.
func TestSingleDeliveryRedeliversOnTailFault(t *testing.T) {
	boom := errors.New("tail fault")
	cases := []struct {
		name      string
		documents *recordingDocumentReceiver
		postings  *recordingPostingReceiver
		sweeper   crawlresults.StalePostingSweeper
		batch     yagocrawlcontract.IngestBatch
	}{
		{
			"document store error", &recordingDocumentReceiver{err: boom},
			&recordingPostingReceiver{}, nil, groupDocBatch("https://example.org/doc", "тело"),
		},
		{
			"stale sweep error", &recordingDocumentReceiver{},
			&recordingPostingReceiver{}, &plainGroupSweeper{err: boom},
			postingsBatch("https://example.org/postings"),
		},
		{
			"posting store busy", &recordingDocumentReceiver{},
			&recordingPostingReceiver{receipt: rwi.Receipt{Busy: true}}, nil,
			groupDocBatch("https://example.org/busy", "тело"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
			consumer := crawlresults.NewIngestConsumer(
				stream, tc.documents, &recordingURLReceiver{}, tc.postings,
			)
			if tc.sweeper != nil {
				consumer.SweepStalePostings(tc.sweeper)
			}
			var wg sync.WaitGroup
			counter := &settleCounter{}
			stream.out <- groupDelivery(tc.batch, &wg, counter, nil)
			drainGroup(consumer, &wg)

			if _, naks := counter.counts(); naks != 1 {
				t.Fatalf("naks = %d, want the single delivery redelivered", naks)
			}
		})
	}
}
