// Package crawling owns the crawlReceipt endpoint, which acknowledges crawl
// receipts from peers. MountCrawlReceipt is its only surface.
package crawling

import (
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

func MountCrawlReceipt(router httpguard.WireRouter) {
	httpguard.Mount(
		router,
		yacyproto.PathCrawlReceipt,
		yacyproto.CrawlReceiptEndpointMethods,
		yacyproto.ParseCrawlReceiptRequest,
		crawlReceiptEndpoint{}.Serve,
	)
}
