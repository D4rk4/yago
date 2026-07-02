package publicweb_test

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yacycrawler/internal/publicweb"
	"github.com/D4rk4/yago/yacyegress"
)

func admissionFetcher(
	inner pagefetch.PageSource,
	resolver publicweb.Resolver,
) *publicweb.AdmissionFetcher {
	return publicweb.NewAdmissionFetcher(inner, resolver, yacyegress.NewGuard(false))
}

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(
	ctx context.Context,
	network string,
	host string,
) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

type fetchFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f fetchFunc) Fetch(ctx context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

func TestAdmissionFetcherAllowsPublicWebTargets(t *testing.T) {
	innerCalls := 0
	fetcher := admissionFetcher(
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			innerCalls++
			return pagefetch.FetchedPage{
				URL:         target,
				ContentType: "text/html",
				Body:        []byte("<html></html>"),
			}, nil
		}),
		resolverFunc(func(_ context.Context, network, host string) ([]netip.Addr, error) {
			if network != "ip" || host != "example.com" {
				t.Fatalf("lookup = %q %q", network, host)
			}
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}),
	)

	page, err := fetcher.Fetch(context.Background(), mustParse(t, "https://example.com/path"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if innerCalls != 1 {
		t.Fatalf("inner calls = %d", innerCalls)
	}
	if page.URL.String() != "https://example.com/path" {
		t.Fatalf("url = %q", page.URL)
	}
}

func TestAdmissionFetcherAllowsPrivateWhenGuardPermits(t *testing.T) {
	innerCalls := 0
	fetcher := publicweb.NewAdmissionFetcher(
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			innerCalls++
			return pagefetch.FetchedPage{URL: target}, nil
		}),
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("10.0.0.2")}, nil
		}),
		yacyegress.NewGuard(true),
	)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://intranet.example/"),
	); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if innerCalls != 1 {
		t.Fatalf("inner calls = %d", innerCalls)
	}
}

func TestNewAdmissionFetcherAcceptsDefaultResolver(t *testing.T) {
	fetcher := admissionFetcher(
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, nil
		}),
		nil,
	)
	if fetcher == nil {
		t.Fatal("fetcher is nil")
	}
}

func TestAdmissionFetcherAllowsPublicLiteralAddress(t *testing.T) {
	innerCalls := 0
	fetcher := admissionFetcher(
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			innerCalls++
			return pagefetch.FetchedPage{URL: target}, nil
		}),
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			t.Fatal("literal public target must not require DNS")
			return nil, nil
		}),
	)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "https://93.184.216.34/"),
	); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if innerCalls != 1 {
		t.Fatalf("inner calls = %d", innerCalls)
	}
}

func TestAdmissionFetcherRejectsUnsafeTargetsBeforeInnerFetch(t *testing.T) {
	for _, raw := range []string{
		"ftp://example.com/",
		"http://localhost/",
		"http://app.localhost/",
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://192.0.2.10/",
		"http://[::1]/",
		"http://[fe80::1%25eth0]/",
	} {
		t.Run(raw, func(t *testing.T) {
			fetcher := admissionFetcher(
				fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
					t.Fatal("inner fetch must not run")
					return pagefetch.FetchedPage{}, nil
				}),
				resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
					t.Fatal("literal or local target must not require DNS")
					return nil, nil
				}),
			)

			_, err := fetcher.Fetch(context.Background(), mustParse(t, raw))
			if !errors.Is(err, pagefetch.ErrPageRejected) {
				t.Fatalf("error = %v, want page rejected", err)
			}
		})
	}
}

func TestAdmissionFetcherRejectsMalformedTargetsBeforeInnerFetch(t *testing.T) {
	for _, target := range []*url.URL{
		nil,
		{Scheme: "http"},
		{Scheme: "http", Host: ":"},
	} {
		fetcher := admissionFetcher(
			fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
				t.Fatal("inner fetch must not run")
				return pagefetch.FetchedPage{}, nil
			}),
			resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
				t.Fatal("malformed target must not require DNS")
				return nil, nil
			}),
		)

		_, err := fetcher.Fetch(context.Background(), target)
		if !errors.Is(err, pagefetch.ErrPageRejected) {
			t.Fatalf("target = %#v error = %v, want page rejected", target, err)
		}
	}
}

func TestAdmissionFetcherRejectsUnsafeDNSAnswersBeforeInnerFetch(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.2")},
		{netip.MustParseAddr("198.51.100.10")},
		{netip.Addr{}},
		{},
	} {
		innerCalls := 0
		fetcher := admissionFetcher(
			fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
				innerCalls++
				return pagefetch.FetchedPage{}, nil
			}),
			resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
				return addresses, nil
			}),
		)

		_, err := fetcher.Fetch(context.Background(), mustParse(t, "https://example.com/"))
		if !errors.Is(err, pagefetch.ErrPageRejected) {
			t.Fatalf("addresses = %v error = %v, want page rejected", addresses, err)
		}
		if innerCalls != 0 {
			t.Fatalf("inner calls = %d", innerCalls)
		}
	}
}

func TestAdmissionFetcherReturnsInnerFetchError(t *testing.T) {
	sentinel := errors.New("fetch failed")
	fetcher := admissionFetcher(
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, sentinel
		}),
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}),
	)

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "https://example.com/"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestAdmissionFetcherRejectsResolverFailureBeforeInnerFetch(t *testing.T) {
	innerCalls := 0
	fetcher := admissionFetcher(
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			innerCalls++
			return pagefetch.FetchedPage{}, nil
		}),
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return nil, errors.New("dns failed")
		}),
	)

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "https://example.com/"))
	if !errors.Is(err, pagefetch.ErrPageRejected) ||
		!strings.Contains(err.Error(), "dns failed") {
		t.Fatalf("error = %v, want resolver rejection", err)
	}
	if innerCalls != 0 {
		t.Fatalf("inner calls = %d", innerCalls)
	}
}

func TestAdmissionFetcherRejectsUnsafeFinalURL(t *testing.T) {
	innerCalls := 0
	fetcher := admissionFetcher(
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			innerCalls++
			return pagefetch.FetchedPage{
				URL: mustParse(t, "http://127.0.0.1/admin"),
			}, nil
		}),
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}),
	)

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "https://example.com/"))
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, want page rejected", err)
	}
	if innerCalls != 1 {
		t.Fatalf("inner calls = %d", innerCalls)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}
