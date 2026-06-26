package pagefetch

import (
	"context"
	"errors"
	"net/url"
)

var ErrPageRejected = errors.New("page rejected")

type FetchedPage struct {
	URL         *url.URL
	ContentType string
	Body        []byte
}

type PageSource interface {
	Fetch(ctx context.Context, target *url.URL) (FetchedPage, error)
}
