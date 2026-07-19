package crawldenylist_test

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type fetchFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f fetchFunc) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

func TestDenylistRetainsLastGoodPolicy(t *testing.T) {
	denylist := crawldenylist.New()
	if denylist.Ready() || denylist.Revision() != nil ||
		!denylist.Blocks("https://allowed.example/") {
		t.Fatal("uninitialized denylist did not fail closed")
	}
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(
		[]string{"https://exact.example/blocked"},
		[]string{"blocked.example"},
	)
	if err != nil {
		t.Fatalf("build policy: %v", err)
	}
	if err := denylist.Apply(policy); err != nil {
		t.Fatalf("apply policy: %v", err)
	}
	if err := denylist.Apply(policy); err != nil {
		t.Fatalf("reapply policy: %v", err)
	}
	if !denylist.Ready() || !denylist.Wait(t.Context()) ||
		!denylist.Blocks("https://exact.example/blocked") ||
		!denylist.Blocks("https://sub.blocked.example/page") ||
		denylist.Blocks("https://allowed.example/") {
		t.Fatal("applied denylist did not enforce exact URL and domain entries")
	}
	wantRevision := denylist.Revision()
	readRevision := denylist.Revision()
	readRevision[0] ^= 0xff
	if !bytes.Equal(denylist.Revision(), wantRevision) {
		t.Fatal("caller mutated denylist revision")
	}
	policy.Revision[0] ^= 0xff
	if err := denylist.Apply(policy); err == nil {
		t.Fatal("corrupt policy accepted")
	}
	if !bytes.Equal(denylist.Revision(), wantRevision) ||
		!denylist.Blocks("https://sub.blocked.example/page") {
		t.Fatal("corrupt update replaced the last-good policy")
	}
}

func TestDenylistWaitObservesFirstPolicy(t *testing.T) {
	denylist := crawldenylist.New()
	ready := make(chan bool, 1)
	go func() { ready <- denylist.Wait(t.Context()) }()
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(nil, nil)
	if err != nil {
		t.Fatalf("build policy: %v", err)
	}
	if err := denylist.Apply(policy); err != nil {
		t.Fatalf("apply policy: %v", err)
	}
	if !<-ready {
		t.Fatal("waiter did not observe first policy")
	}
}

func TestDenylistWaitStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if crawldenylist.New().Wait(ctx) {
		t.Fatal("uninitialized denylist ignored context cancellation")
	}
}

func TestAdmissionFetcherRejectsBeforeAndAfterInnerFetch(t *testing.T) {
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(
		[]string{"https://blocked.example/exact"},
		[]string{"redirected.example"},
	)
	if err != nil {
		t.Fatalf("build policy: %v", err)
	}
	denylist := crawldenylist.New()
	if err := denylist.Apply(policy); err != nil {
		t.Fatalf("apply policy: %v", err)
	}
	calls := 0
	fetcher := crawldenylist.NewAdmissionFetcher(fetchFunc(func(
		context.Context,
		*url.URL,
	) (pagefetch.FetchedPage, error) {
		calls++

		return pagefetch.FetchedPage{URL: mustParse(t, "https://redirected.example/")}, nil
	}), denylist)

	_, err = fetcher.Fetch(t.Context(), mustParse(t, "https://blocked.example/exact"))
	assertPermanentRejection(t, err)
	if calls != 0 {
		t.Fatalf("blocked target reached inner fetch %d times", calls)
	}
	_, err = fetcher.Fetch(t.Context(), mustParse(t, "https://allowed.example/"))
	assertPermanentRejection(t, err)
	if calls != 1 {
		t.Fatalf("redirect result inner calls = %d, want 1", calls)
	}
	_, err = fetcher.Fetch(t.Context(), nil)
	assertPermanentRejection(t, err)
	if err.Error() == "" {
		t.Fatal("nil target rejection has empty message")
	}
}

func TestAdmissionFetcherReturnsAllowedPageAndInnerError(t *testing.T) {
	policy, err := yagocrawlcontract.NewCrawlURLDenylist(nil, nil)
	if err != nil {
		t.Fatalf("build policy: %v", err)
	}
	denylist := crawldenylist.New()
	if err := denylist.Apply(policy); err != nil {
		t.Fatalf("apply policy: %v", err)
	}
	target := mustParse(t, "https://allowed.example/")
	fetcher := crawldenylist.NewAdmissionFetcher(fetchFunc(func(
		context.Context,
		*url.URL,
	) (pagefetch.FetchedPage, error) {
		return pagefetch.FetchedPage{URL: target}, nil
	}), denylist)
	page, err := fetcher.Fetch(t.Context(), target)
	if err != nil || page.URL.String() != target.String() {
		t.Fatalf("allowed fetch = %+v, %v", page, err)
	}
	want := errors.New("inner failed")
	fetcher = crawldenylist.NewAdmissionFetcher(fetchFunc(func(
		context.Context,
		*url.URL,
	) (pagefetch.FetchedPage, error) {
		return pagefetch.FetchedPage{}, want
	}), denylist)
	if _, err := fetcher.Fetch(t.Context(), target); !errors.Is(err, want) {
		t.Fatalf("inner error = %v, want %v", err, want)
	}
	if denylist.Blocks("%zz") || denylist.Blocks("mailto:person@example.com") {
		t.Fatal("hostless or malformed URL was blocked by an empty policy")
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	return parsed
}

func assertPermanentRejection(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, want page rejection", err)
	}
	var permanent interface{ Permanent() bool }
	if !errors.As(err, &permanent) || !permanent.Permanent() {
		t.Fatalf("error = %v, want permanent rejection", err)
	}
}
