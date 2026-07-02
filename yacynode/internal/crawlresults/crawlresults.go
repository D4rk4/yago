// Package crawlresults absorbs ingest batches returned by the crawl fleet,
// storing their URL metadata and then their postings through the node's existing
// receivers. NewIngestConsumer and its Run loop are the only surface; IngestStream
// is the port batches arrive through.
package crawlresults

import (
	"context"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/searchindex"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
)

type IngestDelivery struct {
	Batch yacycrawlcontract.IngestBatch
	Ack   func(context.Context) error
	Nak   func(context.Context) error
}

type IngestStream interface {
	Receive() <-chan IngestDelivery
}

type IngestConsumer struct {
	stream    IngestStream
	documents documentstore.DocumentReceiver
	index     searchindex.SearchIndex
	urls      urlmeta.URLReceiver
	postings  rwi.PostingReceiver
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
	}
}
