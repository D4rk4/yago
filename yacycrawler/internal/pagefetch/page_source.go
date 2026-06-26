package pagefetch

import "context"

type FetchedPage struct {
	URL         string
	ContentType string
	Body        []byte
}

type PageSource interface {
	Fetch(ctx context.Context, rawURL string) (FetchedPage, error)
}
