// Package crawling owns the crawlReceipt endpoint, which rejects remote crawl
// receipts until an operator policy enables remote crawl work.
package crawling

import (
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type LocalPeer interface {
	NetworkMatches(network string) bool
	Addresses(network string, youare yagomodel.Hash) bool
}

func MountCrawlReceipt(router httpguard.WireRouter, local LocalPeer) {
	httpguard.Mount(
		router,
		yagoproto.PathCrawlReceipt,
		yagoproto.CrawlReceiptEndpointMethods,
		yagoproto.ParseCrawlReceiptRequest,
		disabledCrawlReceiptEndpoint{local: local}.Serve,
	)
}
