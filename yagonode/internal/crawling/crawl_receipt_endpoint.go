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
	if e.local == nil || !e.local.AuthenticatesAddress(
		req.NetworkName,
		req.YouAre,
		req.Key,
		req.Iam.String(),
		req.MagicMD5,
	) {
		return yagoproto.CrawlReceiptResponse{Delay: disabledCrawlReceiptRetryDelay}, nil
	}

	slog.DebugContext(
		ctx,
		logCrawlReceiptRejected,
		slog.String(crawlReceiptRejectReasonKey, crawlReceiptRejectDisabled),
	)

	return yagoproto.CrawlReceiptResponse{Delay: disabledCrawlReceiptRetryDelay}, nil
}
