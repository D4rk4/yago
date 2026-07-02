package pagefetch_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
)

type sourceFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f sourceFunc) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

func TestFallbackPageSourceReturnsPrimaryPage(t *testing.T) {
	fallbackCalls := 0
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{ContentType: "text/html"}, nil
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			fallbackCalls++
			return pagefetch.FetchedPage{}, nil
		}),
	)

	page, err := source.Fetch(context.Background(), exampleURL(t))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ContentType != "text/html" {
		t.Fatalf("content type = %q", page.ContentType)
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d", fallbackCalls)
	}
}

func TestFallbackPageSourceUsesFallbackForRejectedPrimary(t *testing.T) {
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, pagefetch.ErrPageRejected
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{ContentType: "browser"}, nil
		}),
	)

	page, err := source.Fetch(context.Background(), exampleURL(t))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ContentType != "browser" {
		t.Fatalf("content type = %q", page.ContentType)
	}
}

func TestFallbackPageSourceReturnsPrimaryFetchError(t *testing.T) {
	sentinel := errors.New("network failed")
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, sentinel
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			t.Fatal("fallback must not run")
			return pagefetch.FetchedPage{}, nil
		}),
	)

	_, err := source.Fetch(context.Background(), exampleURL(t))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestFallbackPageSourceReturnsFallbackFetchError(t *testing.T) {
	sentinel := errors.New("browser failed")
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, pagefetch.ErrPageRejected
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, sentinel
		}),
	)

	_, err := source.Fetch(context.Background(), exampleURL(t))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func exampleURL(t *testing.T) *url.URL {
	t.Helper()
	parsed, err := url.Parse("https://example.com/")
	if err != nil {
		t.Fatalf("parse example URL: %v", err)
	}
	return parsed
}
