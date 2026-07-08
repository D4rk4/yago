package crawlcapability

import "context"

type RecrawlDecision interface {
	Due(ctx context.Context, canonicalURL string) (bool, error)
}
