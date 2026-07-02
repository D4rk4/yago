// Package crawling owns the crawlReceipt endpoint, which rejects remote crawl
// receipts until an operator policy enables remote crawl work.
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
		disabledCrawlReceiptEndpoint{}.Serve,
	)
}
