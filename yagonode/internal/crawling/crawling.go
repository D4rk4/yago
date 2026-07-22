// Package crawling owns the crawlReceipt endpoint, which rejects remote crawl
// receipts until an operator policy enables remote crawl work.
package crawling

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type LocalPeer interface {
	Authenticates(
		network string,
		networkPresent bool,
		key string,
		iam string,
		magic string,
	) bool
	Addresses(network string, youare yagomodel.Hash) bool
}

type ReceiptProcessor interface {
	ProcessReceipt(
		context.Context,
		yagoproto.CrawlReceiptRequest,
	) (yagoproto.CrawlReceiptResponse, error)
}

func MountCrawlReceipt(
	router httpguard.WireRouter,
	local LocalPeer,
	processors ...ReceiptProcessor,
) {
	var endpoint func(
		context.Context,
		yagoproto.CrawlReceiptRequest,
	) (yagoproto.CrawlReceiptResponse, error)
	if len(processors) > 0 && processors[0] != nil {
		endpoint = enabledCrawlReceiptEndpoint{local: local, processor: processors[0]}.Serve
	} else {
		endpoint = disabledCrawlReceiptEndpoint{local: local}.Serve
	}
	httpguard.Mount(
		router,
		yagoproto.PathCrawlReceipt,
		yagoproto.CrawlReceiptEndpointMethods,
		yagoproto.ParseCrawlReceiptRequest,
		endpoint,
	)
}
