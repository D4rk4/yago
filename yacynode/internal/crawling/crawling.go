// Package crawling owns the crawlReceipt endpoint, which rejects remote crawl
// receipts until an operator policy enables remote crawl work.
package crawling

import (
	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type LocalPeer interface {
	NetworkMatches(network string) bool
	Addresses(network string, youare yacymodel.Hash) bool
}

func MountCrawlReceipt(router httpguard.WireRouter, local LocalPeer) {
	httpguard.Mount(
		router,
		yacyproto.PathCrawlReceipt,
		yacyproto.CrawlReceiptEndpointMethods,
		yacyproto.ParseCrawlReceiptRequest,
		disabledCrawlReceiptEndpoint{local: local}.Serve,
	)
}
