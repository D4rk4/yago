// Package crawling owns the crawlReceipt endpoint, which acknowledges crawl
// receipts from peers. MountCrawlReceipt is its only surface.
package crawling

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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
