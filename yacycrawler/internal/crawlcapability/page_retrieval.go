package crawlcapability

import "context"

type PageRetrieval interface {
	Fetch(ctx context.Context, rawURL string) (FetchOutcome, error)
}
