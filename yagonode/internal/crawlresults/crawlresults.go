// Package crawlresults absorbs ingest batches returned by the crawl fleet,
// storing their URL metadata and then their postings through the node's existing
// receivers. NewIngestConsumer and its Run loop are the only surface; IngestStream
// is the port batches arrive through.
package crawlresults

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/contentsafety"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type IngestDelivery struct {
	Batch yagocrawlcontract.IngestBatch
	Ack   func(context.Context) error
	Nak   func(context.Context) error
}

type IngestStream interface {
	Receive() <-chan IngestDelivery
}

type IngestObserver interface {
	ObserveAbsorbed(contentBytes, urls, postings int)
	ObserveDeferred()
	ObserveRejected()
	ObserveLowQuality()
}

type noopIngestObserver struct{}

func (noopIngestObserver) ObserveAbsorbed(int, int, int) {}

func (noopIngestObserver) ObserveDeferred() {}

func (noopIngestObserver) ObserveRejected() {}

func (noopIngestObserver) ObserveLowQuality() {}

// FetchRecorder is told, once per successfully absorbed page batch, which URL was
// fetched under which profile and when, so a recrawl schedule can be maintained.
// It is called only after the batch is durably absorbed, is best-effort (a failure
// never fails the ingest), and must not block.
type FetchRecorder interface {
	RecordFetch(ctx context.Context, url, profileHandle string, fetchedAt time.Time) error
}

type noopFetchRecorder struct{}

func (noopFetchRecorder) RecordFetch(context.Context, string, string, time.Time) error {
	return nil
}

// OwnershipCheck reports whether the node dispatched a crawl under a profile
// handle. Ingest batches naming a handle the node never dispatched are
// unsolicited and are dropped rather than absorbed.
type OwnershipCheck interface {
	OwnsProfile(ctx context.Context, profileHandle string) (bool, error)
}

type allowAllOwnership struct{}

func (allowAllOwnership) OwnsProfile(context.Context, string) (bool, error) {
	return true, nil
}

// URLPurger drops a URL's postings and metadata from the index. A dead-page
// tombstone (ADR-0034) purges its source URL through this port; the assembly
// satisfies it with the eviction purge primitive. Purging an unindexed URL is a
// no-op, so a tombstone is idempotent and safe to redeliver.
type URLPurger interface {
	Purge(ctx context.Context, urls []yagomodel.Hash) error
}

type noopURLPurger struct{}

func (noopURLPurger) Purge(context.Context, []yagomodel.Hash) error { return nil }

// StalePostingSweeper drops a URL's postings for words absent from a fresh
// ingest's set (RWI-01), so a recrawled page stops answering searches for
// words it no longer contains. The assembly satisfies it with the eviction
// primitive; the no-op default keeps bare test consumers working.
type StalePostingSweeper interface {
	PurgeStalePostings(
		ctx context.Context,
		url yagomodel.Hash,
		live map[yagomodel.Hash]struct{},
	) (int, error)
}

type noopStaleSweeper struct{}

func (noopStaleSweeper) PurgeStalePostings(
	context.Context,
	yagomodel.Hash,
	map[yagomodel.Hash]struct{},
) (int, error) {
	return 0, nil
}

type IngestConsumer struct {
	stream       IngestStream
	documents    documentstore.DocumentReceiver
	anchors      documentstore.InboundAnchorReceiver
	index        searchindex.SearchIndex
	urls         urlmeta.URLReceiver
	postings     rwi.PostingReceiver
	observer     IngestObserver
	recorder     FetchRecorder
	owner        OwnershipCheck
	purger       URLPurger
	stale        StalePostingSweeper
	clusters     ContentClusters
	safety       ContentSafetyClassifier
	observations observationHistory
	// quality names the rule a document's text violates, "" for indexable text;
	// nil skips the gate.
	quality func(text string) string
	// hashURL derives a tombstone's URL hash; a field so a test can force the
	// (otherwise unreachable) hashing failure.
	hashURL func(string) (yagomodel.URLHash, error)
}

type ContentSafetyClassifier interface {
	Classify(text string) contentsafety.Evidence
}

func NewIngestConsumer(
	stream IngestStream,
	documents documentstore.DocumentReceiver,
	urls urlmeta.URLReceiver,
	postings rwi.PostingReceiver,
) *IngestConsumer {
	return NewIngestConsumerWithIndex(stream, documents, nil, urls, postings)
}

func NewIngestConsumerWithIndex(
	stream IngestStream,
	documents documentstore.DocumentReceiver,
	index searchindex.SearchIndex,
	urls urlmeta.URLReceiver,
	postings rwi.PostingReceiver,
) *IngestConsumer {
	anchors, _ := documents.(documentstore.InboundAnchorReceiver)

	return &IngestConsumer{
		stream:       stream,
		documents:    documents,
		anchors:      anchors,
		index:        index,
		urls:         urls,
		postings:     postings,
		observer:     noopIngestObserver{},
		recorder:     noopFetchRecorder{},
		owner:        allowAllOwnership{},
		purger:       noopURLPurger{},
		stale:        noopStaleSweeper{},
		hashURL:      yagomodel.HashURL,
		observations: acceptAllObservationHistory{},
	}
}

// Observe installs an observer that receives each batch's ingest outcome. A nil
// observer is ignored so the consumer keeps its silent default.
func (c *IngestConsumer) Observe(observer IngestObserver) {
	if observer != nil {
		c.observer = observer
	}
}

// GateQuality installs a content-quality rule check: a batch whose document
// text violates a rule is dropped whole — spam postings must not reach the
// shared index — with the rejection counted and the rule named in the log
// (CRAWL-14). A nil gate is ignored.
func (c *IngestConsumer) GateQuality(gate func(text string) string) {
	if gate != nil {
		c.quality = gate
	}
}

func (c *IngestConsumer) UseContentSafetyClassifier(classifier ContentSafetyClassifier) {
	if classifier != nil {
		c.safety = classifier
	}
}

// RecordFetches installs a recorder fed the URL, profile handle, and fetch time of
// each absorbed page batch. A nil recorder is ignored so the consumer keeps its
// silent default.
func (c *IngestConsumer) RecordFetches(recorder FetchRecorder) {
	if recorder != nil {
		c.recorder = recorder
	}
}

// CheckOwnership installs an ownership check so batches naming a profile handle
// the node never dispatched are dropped. A nil check is ignored so the consumer
// keeps its default of accepting every batch.
func (c *IngestConsumer) CheckOwnership(oracle OwnershipCheck) {
	if oracle != nil {
		c.owner = oracle
	}
}

// PurgeURLs installs the purge port a dead-page tombstone drops its source URL
// through (ADR-0034). A nil purger is ignored so the consumer keeps its no-op
// default.
func (c *IngestConsumer) PurgeURLs(purger URLPurger) {
	if purger != nil {
		c.purger = purger
	}
}

// SweepStalePostings installs the stale-posting sweeper a fresh ingest runs
// before storing its postings (RWI-01). A nil sweeper keeps the no-op default.
func (c *IngestConsumer) SweepStalePostings(sweeper StalePostingSweeper) {
	if sweeper != nil {
		c.stale = sweeper
	}
}
