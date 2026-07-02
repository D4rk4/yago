// Package crawldispatch turns an operator's request to crawl seed URLs into a
// CrawlOrder and hands it to the crawl fleet. MountCrawlDispatch is its only
// surface; CrawlOrderQueue is the port the order leaves through.
package crawldispatch

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacymodel"
)

const PathCrawlDispatch = "/crawl"

type CrawlOrderQueue interface {
	Publish(ctx context.Context, order yacycrawlcontract.CrawlOrder) error
}

type ProvenanceMint func() []byte

func MountCrawlDispatch(
	mux *http.ServeMux,
	initiator yacymodel.Hash,
	mint ProvenanceMint,
	queue CrawlOrderQueue,
) {
	mux.Handle(PathCrawlDispatch, crawlDispatchEndpoint{
		initiator: initiator,
		mint:      mint,
		queue:     queue,
	})
}
