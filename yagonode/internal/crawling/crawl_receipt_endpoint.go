package crawling

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagoproto"
)

const (
	logCrawlReceiptRejected        = "crawl receipt rejected"
	crawlReceiptRejectReasonKey    = "reason"
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
	if e.local == nil || !e.local.Authenticates(
		req.NetworkName,
		req.NetworkNamePresent,
		req.Key,
		req.Iam.String(),
		req.MagicMD5,
	) || !e.local.Addresses(req.NetworkName, req.YouAre) {
		return yagoproto.CrawlReceiptResponse{Delay: disabledCrawlReceiptRetryDelay}, nil
	}

	slog.DebugContext(
		ctx,
		logCrawlReceiptRejected,
		slog.String(crawlReceiptRejectReasonKey, crawlReceiptRejectDisabled),
	)

	return yagoproto.CrawlReceiptResponse{Delay: disabledCrawlReceiptRetryDelay}, nil
}
