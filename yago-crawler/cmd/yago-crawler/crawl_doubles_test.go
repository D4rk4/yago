package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

type pageSourceFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f pageSourceFunc) Fetch(ctx context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

func htmlPageSource(pages map[string]string) pageSourceFunc {
	return func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
		path := target.Path
		if path == "" {
			path = "/"
		}
		body, ok := pages[path]
		if !ok {
			return pagefetch.FetchedPage{}, fmt.Errorf("missing test page: %s", path)
		}
		return pagefetch.FetchedPage{
			URL:         target,
			ContentType: "text/html",
			Body:        []byte("<html><body>" + body + "</body></html>"),
		}, nil
	}
}
