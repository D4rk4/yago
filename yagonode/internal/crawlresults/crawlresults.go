// Package crawlresults absorbs ingest batches returned by the crawl fleet,
// storing their URL metadata and then their postings through the node's existing
// receivers. NewIngestConsumer and its Run loop are the only surface; IngestStream
// is the port batches arrive through.
package crawlresults

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
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

// IngestObserver receives the node-side outcome of each crawl ingest batch so an
// edge can meter crawl throughput. Its methods are called once per batch and must
// not block. ObserveRejected counts a malformed batch that was dropped rather than
// absorbed or deferred.
type IngestObserver interface {
	ObserveAbsorbed(contentBytes, urls, postings int)
	ObserveDeferred()
	ObserveRejected()
}

type noopIngestObserver struct{}

func (noopIngestObserver) ObserveAbsorbed(int, int, int) {}

func (noopIngestObserver) ObserveDeferred() {}

func (noopIngestObserver) ObserveRejected() {}

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

type IngestConsumer struct {
	stream    IngestStream
	documents documentstore.DocumentReceiver
	index     searchindex.SearchIndex
	urls      urlmeta.URLReceiver
	postings  rwi.PostingReceiver
	observer  IngestObserver
	recorder  FetchRecorder
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
	return &IngestConsumer{
		stream:    stream,
		documents: documents,
		index:     index,
		urls:      urls,
		postings:  postings,
		observer:  noopIngestObserver{},
		recorder:  noopFetchRecorder{},
	}
}

// Observe installs an observer that receives each batch's ingest outcome. A nil
// observer is ignored so the consumer keeps its silent default.
func (c *IngestConsumer) Observe(observer IngestObserver) {
	if observer != nil {
		c.observer = observer
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
