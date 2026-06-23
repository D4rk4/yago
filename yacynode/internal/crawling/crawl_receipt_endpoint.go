package crawling

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type crawlReceiptEndpoint struct{}

func (crawlReceiptEndpoint) Serve(
	ctx context.Context,
	_ yacyproto.CrawlReceiptRequest,
) (yacyproto.CrawlReceiptResponse, error) {
	slog.DebugContext(ctx, "crawl receipt rejected")

	return yacyproto.CrawlReceiptResponse{}, nil
}
