package publicweb_test

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

// Regression: anticisco.ru publishes a real A record next to a bogus "::"
// AAAA record; the whole host was rejected before robots or fetch, and the
// run finished all-zero.
func TestAdmissionFetcherIgnoresUnspecifiedDNSRecordsAlongsideRealOnes(t *testing.T) {
	innerCalls := 0
	fetcher := admissionFetcher(
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			innerCalls++
			return pagefetch.FetchedPage{URL: target}, nil
		}),
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{
				netip.MustParseAddr("62.113.86.44"),
				netip.MustParseAddr("::"),
			}, nil
		}),
	)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "https://anticisco.example/"),
	); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if innerCalls != 1 {
		t.Fatalf("inner calls = %d, want 1", innerCalls)
	}
}

func TestAdmissionFetcherStillRejectsHostsWithOnlyUnspecifiedRecords(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("::")},
		{netip.MustParseAddr("0.0.0.0")},
		{netip.MustParseAddr("::"), netip.MustParseAddr("0.0.0.0")},
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

		_, err := fetcher.Fetch(context.Background(), mustParse(t, "https://dead.example/"))
		if !errors.Is(err, pagefetch.ErrPageRejected) || innerCalls != 0 {
			t.Fatalf(
				"addresses = %v: err = %v innerCalls = %d, want rejected before fetch",
				addresses,
				err,
				innerCalls,
			)
		}
	}
}

func TestAdmissionFetcherKeepsRebindingDefenseDespiteUnspecifiedRecords(t *testing.T) {
	// A private address in the answer still condemns the host even when a
	// bogus unspecified record is present too.
	fetcher := admissionFetcher(
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			t.Fatal("inner fetch must not run")

			return pagefetch.FetchedPage{}, nil
		}),
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{
				netip.MustParseAddr("::"),
				netip.MustParseAddr("10.0.0.2"),
			}, nil
		}),
	)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "https://rebind.example/"),
	); !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("err = %v, want page rejected", err)
	}
}
