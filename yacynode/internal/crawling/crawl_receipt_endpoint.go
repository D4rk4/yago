package crawling

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yacyproto"
)

const logCrawlReceiptRejected = "crawl receipt rejected"

type disabledCrawlReceiptEndpoint struct{}

func (disabledCrawlReceiptEndpoint) Serve(
	ctx context.Context,
	_ yacyproto.CrawlReceiptRequest,
) (yacyproto.CrawlReceiptResponse, error) {
	slog.DebugContext(ctx, logCrawlReceiptRejected)

	return yacyproto.CrawlReceiptResponse{}, nil
}
