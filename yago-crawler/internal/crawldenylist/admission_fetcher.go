package crawldenylist

import (
	"context"
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

type AdmissionFetcher struct {
	inner    pagefetch.PageSource
	denylist *Denylist
}

type deniedError struct {
	url string
}

func NewAdmissionFetcher(
	inner pagefetch.PageSource,
	denylist *Denylist,
) *AdmissionFetcher {
	return &AdmissionFetcher{inner: inner, denylist: denylist}
}

func (f *AdmissionFetcher) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	if target == nil || f.denylist.Blocks(target.String()) {
		return pagefetch.FetchedPage{}, deniedError{url: targetString(target)}
	}
	page, err := f.inner.Fetch(ctx, target)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("crawl URL denylist fetch: %w", err)
	}
	if page.URL == nil || f.denylist.Blocks(page.URL.String()) {
		return pagefetch.FetchedPage{}, deniedError{url: targetString(page.URL)}
	}

	return page, nil
}

func (e deniedError) Error() string {
	return fmt.Sprintf("crawl URL %q denied: %v", e.url, pagefetch.ErrPageRejected)
}

func (e deniedError) Unwrap() error {
	return pagefetch.ErrPageRejected
}

func (deniedError) Permanent() bool {
	return true
}

func targetString(target *url.URL) string {
	if target == nil {
		return ""
	}

	return target.String()
}
