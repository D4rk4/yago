package crawling

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagoproto"
)

type enabledCrawlReceiptEndpoint struct {
	local     LocalPeer
	processor ReceiptProcessor
}

func (e enabledCrawlReceiptEndpoint) Serve(
	ctx context.Context,
	req yagoproto.CrawlReceiptRequest,
) (yagoproto.CrawlReceiptResponse, error) {
	if e.local == nil || e.processor == nil || !e.local.AuthenticatesAddress(
		req.NetworkName,
		req.YouAre,
		req.Key,
		req.Iam.String(),
		req.MagicMD5,
	) {
		return yagoproto.CrawlReceiptResponse{Delay: disabledCrawlReceiptRetryDelay}, nil
	}

	response, err := e.processor.ProcessReceipt(ctx, req)
	if err != nil {
		return yagoproto.CrawlReceiptResponse{}, fmt.Errorf("process crawl receipt: %w", err)
	}

	return response, nil
}
