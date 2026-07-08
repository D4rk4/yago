package crawlcapability

import "context"

type PagePublication interface {
	Name() string
	Publish(ctx context.Context, page ExtractedPage) error
}
