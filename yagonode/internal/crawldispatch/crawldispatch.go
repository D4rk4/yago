// Package crawldispatch turns an operator's request to crawl seed URLs into a
// CrawlOrder and hands it to the crawl fleet. MountCrawlDispatch is its only
// surface; CrawlOrderQueue is the port the order leaves through.
package crawldispatch

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

const PathCrawlDispatch = "/crawl"

// CrawlOrderQueue accepts crawl orders for durable delivery. When key is
// non-empty and was already accepted, the order is not enqueued again and
// duplicate is true; an empty key disables idempotency.
type CrawlOrderQueue interface {
	PublishOnce(
		ctx context.Context,
		key string,
		order yagocrawlcontract.CrawlOrder,
	) (duplicate bool, err error)
}

type ProvenanceMint func() []byte

func MountCrawlDispatch(
	mux *http.ServeMux,
	initiator yagomodel.Hash,
	mint ProvenanceMint,
	queue CrawlOrderQueue,
	options ...DispatcherOption,
) {
	mux.Handle(PathCrawlDispatch, crawlDispatchEndpoint{
		dispatcher: NewDispatcher(initiator, mint, queue, options...),
	})
}
