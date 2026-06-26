package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

type pageSourceFunc func(context.Context, string) (pagefetch.FetchedPage, error)

func (f pageSourceFunc) Fetch(ctx context.Context, rawURL string) (pagefetch.FetchedPage, error) {
	return f(ctx, rawURL)
}

func htmlPageSource(pages map[string]string) pageSourceFunc {
	return func(_ context.Context, rawURL string) (pagefetch.FetchedPage, error) {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return pagefetch.FetchedPage{}, fmt.Errorf("parse url: %w", err)
		}
		path := parsed.Path
		if path == "" {
			path = "/"
		}
		body, ok := pages[path]
		if !ok {
			return pagefetch.FetchedPage{}, fmt.Errorf("missing test page: %s", path)
		}
		return pagefetch.FetchedPage{
			URL:         rawURL,
			ContentType: "text/html",
			Body:        []byte("<html><body>" + body + "</body></html>"),
		}, nil
	}
}
