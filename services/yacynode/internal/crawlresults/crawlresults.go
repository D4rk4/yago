// Package crawlresults absorbs ingest messages returned by the crawl fleet. Each
// message carries either a page's URL metadata or one bounded batch of its postings,
// stored through the node's existing receivers. NewIngestConsumer and its Run loop
// are the only surface; IngestStream is the port messages arrive through.
package crawlresults

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

type IngestDelivery struct {
	Message yacycrawlcontract.CrawledPageIndexMessage
	Ack     func(context.Context) error
	Nak     func(context.Context) error
}

type IngestStream interface {
	Receive() <-chan IngestDelivery
}

type IngestConsumer struct {
	stream   IngestStream
	urls     urlmeta.URLReceiver
	postings rwi.PostingReceiver
}

func NewIngestConsumer(
	stream IngestStream,
	urls urlmeta.URLReceiver,
	postings rwi.PostingReceiver,
) *IngestConsumer {
	return &IngestConsumer{stream: stream, urls: urls, postings: postings}
}
