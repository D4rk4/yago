package pagefetch_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

func TestBrowserFallbackPageSourceRendersSuccessfulShell(t *testing.T) {
	target := exampleURL(t)
	primary := pagefetch.FetchedPage{
		URL: target, HTTPStatus: 200, ContentType: "text/html", Body: []byte("shell"),
	}
	rendered := pagefetch.FetchedPage{
		URL:         target,
		ContentType: "text/html",
		Body:        []byte("rendered"),
	}
	browserCalls := 0
	source := pagefetch.NewBrowserFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return primary, nil
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			browserCalls++
			return rendered, nil
		}),
		func(page pagefetch.FetchedPage) bool { return string(page.Body) == "shell" },
	)
	page, err := source.Fetch(t.Context(), target)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(page.Body) != "rendered" || browserCalls != 1 {
		t.Fatalf("page/browser calls = %q/%d", page.Body, browserCalls)
	}
	if page.HTTPStatus != 200 {
		t.Fatalf("HTTP status = %d, want preserved primary status", page.HTTPStatus)
	}
}

func TestBrowserFallbackPageSourceKeepsRenderedHTTPStatus(t *testing.T) {
	target := exampleURL(t)
	source := pagefetch.NewBrowserFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{URL: target, HTTPStatus: 200, Body: []byte("shell")}, nil
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{URL: target, HTTPStatus: 204}, nil
		}),
		func(pagefetch.FetchedPage) bool { return true },
	)
	page, err := source.Fetch(t.Context(), target)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.HTTPStatus != 204 {
		t.Fatalf("HTTP status = %d, want rendered response status", page.HTTPStatus)
	}
}

func TestBrowserFallbackPageSourceKeepsUsablePrimary(t *testing.T) {
	target := exampleURL(t)
	browserCalls := 0
	source := pagefetch.NewBrowserFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{URL: target, Body: []byte("usable")}, nil
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			browserCalls++
			return pagefetch.FetchedPage{}, nil
		}),
		func(page pagefetch.FetchedPage) bool { return string(page.Body) == "shell" },
	)
	page, err := source.Fetch(t.Context(), target)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(page.Body) != "usable" || browserCalls != 0 {
		t.Fatalf("page/browser calls = %q/%d", page.Body, browserCalls)
	}
}

func TestBrowserFallbackPageSourceHonorsSuccessfulPageOptOut(t *testing.T) {
	target := exampleURL(t)
	primary := pagefetch.FetchedPage{URL: target, Body: []byte("shell")}
	browserCalls := 0
	source := pagefetch.NewBrowserFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return primary, nil
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			browserCalls++
			return pagefetch.FetchedPage{}, nil
		}),
		func(pagefetch.FetchedPage) bool { return true },
	)
	page, err := source.Fetch(
		pagefetch.WithoutBrowserFallback(t.Context()),
		target,
	)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(page.Body) != "shell" || browserCalls != 0 {
		t.Fatalf("page/browser calls = %q/%d", page.Body, browserCalls)
	}
}

func TestBrowserFallbackPageSourceReturnsSuccessfulPageFallbackError(t *testing.T) {
	target := exampleURL(t)
	sentinel := errors.New("browser failed")
	source := pagefetch.NewBrowserFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{URL: target}, nil
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, sentinel
		}),
		func(pagefetch.FetchedPage) bool { return true },
	)
	if _, err := source.Fetch(t.Context(), target); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}
