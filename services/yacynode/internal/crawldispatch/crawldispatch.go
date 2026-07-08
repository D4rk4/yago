// Package crawldispatch turns an operator's request to crawl seed URLs into a
// CrawlOrder and hands it to the crawl fleet. MountCrawlDispatch is its only
// surface; CrawlOrderQueue is the port the order leaves through.
package crawldispatch

import (
	"context"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const PathCrawlDispatch = "/crawl"

type CrawlOrderQueue interface {
	Publish(ctx context.Context, order yacycrawlcontract.CrawlOrder) error
}

func MountCrawlDispatch(mux *http.ServeMux, queue CrawlOrderQueue) {
	mux.Handle(PathCrawlDispatch, crawlDispatchEndpoint{queue: queue})
}
