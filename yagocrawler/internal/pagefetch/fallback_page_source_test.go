package pagefetch_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
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

func TestFallbackPageSourceSkipsBrowserForUnsupportedContentType(t *testing.T) {
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, pagefetch.ErrUnsupportedContentType
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			t.Fatal("browser fallback must not run for non-HTML media")
			return pagefetch.FetchedPage{}, nil
		}),
	)

	_, err := source.Fetch(context.Background(), exampleURL(t))
	if !errors.Is(err, pagefetch.ErrUnsupportedContentType) {
		t.Fatalf("error = %v, want unsupported content type", err)
	}
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, must stay a page rejection", err)
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

// TestFallbackPageSourceSkipsBrowserOnThrottle: a 429/503 is the server asking
// for restraint, so the browser fallback must not hit the host again.
func TestFallbackPageSourceSkipsBrowserOnThrottle(t *testing.T) {
	fallbackCalls := 0
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, &pagefetch.ThrottledError{Status: 429}
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			fallbackCalls++
			return pagefetch.FetchedPage{}, nil
		}),
	)

	_, err := source.Fetch(context.Background(), exampleURL(t))
	if _, ok := pagefetch.AsThrottled(err); !ok {
		t.Fatalf("throttle signal lost: %v", err)
	}
	if fallbackCalls != 0 {
		t.Fatalf("browser fallback ran %d times on a throttled host", fallbackCalls)
	}
}

// TestFallbackPageSourceSkipsBrowserForGonePage: a 404/410 is the server's
// definitive gone verdict, so the browser must not run — it would render the
// error page into a soft-404 document and bury the gone signal, leaving the
// recrawl path unable to tombstone the URL (ADR-0034). The gone status must
// propagate unchanged.
func TestFallbackPageSourceSkipsBrowserForGonePage(t *testing.T) {
	fallbackCalls := 0
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, &pagefetch.GoneError{Status: 404}
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			fallbackCalls++
			return pagefetch.FetchedPage{}, nil
		}),
	)

	_, err := source.Fetch(context.Background(), exampleURL(t))
	if _, ok := pagefetch.AsGone(err); !ok {
		t.Fatalf("gone signal lost through fallback: %v", err)
	}
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, must stay a page rejection", err)
	}
	if fallbackCalls != 0 {
		t.Fatalf("browser fallback ran %d times on a gone page", fallbackCalls)
	}
}

// TestFallbackPageSourceHonorsBrowserOptOut: a profile that disabled browser
// rendering keeps a rejected fast fetch rejected instead of escalating.
func TestFallbackPageSourceHonorsBrowserOptOut(t *testing.T) {
	fallbackCalls := 0
	source := pagefetch.NewFallbackPageSource(
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, pagefetch.ErrPageRejected
		}),
		sourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			fallbackCalls++
			return pagefetch.FetchedPage{}, nil
		}),
	)

	ctx := pagefetch.WithoutBrowserFallback(context.Background())
	if _, err := source.Fetch(ctx, exampleURL(t)); !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("rejection lost: %v", err)
	}
	if fallbackCalls != 0 {
		t.Fatalf("browser ran %d times despite the opt-out", fallbackCalls)
	}
}
