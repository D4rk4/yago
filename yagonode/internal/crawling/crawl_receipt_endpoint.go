package crawling

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagoproto"
)

const (
	logCrawlReceiptRejected        = "crawl receipt rejected"
	crawlReceiptRejectReasonKey    = "reason"
	crawlReceiptRejectWrongTarget  = "wrong_target"
	crawlReceiptRejectDisabled     = "remote_crawl_disabled"
	disabledCrawlReceiptRetryDelay = 3600
)

type disabledCrawlReceiptEndpoint struct {
	local LocalPeer
}

func (e disabledCrawlReceiptEndpoint) Serve(
	ctx context.Context,
	req yagoproto.CrawlReceiptRequest,
) (yagoproto.CrawlReceiptResponse, error) {
	if e.local == nil || !e.local.NetworkMatches(req.NetworkName) {
		return yagoproto.CrawlReceiptResponse{}, nil
	}

	if !e.local.Addresses(req.NetworkName, req.YouAre) {
		slog.DebugContext(
			ctx,
			logCrawlReceiptRejected,
			slog.String(crawlReceiptRejectReasonKey, crawlReceiptRejectWrongTarget),
		)

		return yagoproto.CrawlReceiptResponse{Delay: disabledCrawlReceiptRetryDelay}, nil
	}

	slog.DebugContext(
		ctx,
		logCrawlReceiptRejected,
		slog.String(crawlReceiptRejectReasonKey, crawlReceiptRejectDisabled),
	)

	return yagoproto.CrawlReceiptResponse{Delay: disabledCrawlReceiptRetryDelay}, nil
}
