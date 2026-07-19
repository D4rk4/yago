package main

import (
	"context"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

type maximumRedirectsSource struct {
	maximum int
}

type redirectTestPageSource func(
	context.Context,
	*url.URL,
) (pagefetch.FetchedPage, error)

func (source redirectTestPageSource) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	return source(ctx, target)
}

func (source *maximumRedirectsSource) Fetch(
	context.Context,
	*url.URL,
) (pagefetch.FetchedPage, error) {
	return pagefetch.FetchedPage{}, nil
}

func (source *maximumRedirectsSource) SetMaxRedirects(maximum int) {
	source.maximum = maximum
}

func TestApplyMaximumRedirectsUpdatesHTTPAndBrowserFetchers(t *testing.T) {
	limit := newRedirectLimit(10)
	source := &maximumRedirectsSource{maximum: 10}
	control := maximumRedirectsControl{limit: limit, source: source}
	control.Apply(7)
	if limit.Current() != 7 || source.maximum != 7 {
		t.Fatalf("redirect controls = %d/%d, want 7/7", limit.Current(), source.maximum)
	}
	control.source = redirectTestPageSource(func(
		context.Context,
		*url.URL,
	) (pagefetch.FetchedPage, error) {
		return pagefetch.FetchedPage{}, nil
	})
	control.Apply(4)
	if limit.Current() != 4 {
		t.Fatalf("HTTP redirect limit = %d, want 4", limit.Current())
	}
}
