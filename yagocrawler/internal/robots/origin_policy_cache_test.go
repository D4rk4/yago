package robots

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

type policyPageSource func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (source policyPageSource) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	return source(ctx, target)
}

type policyRoundTripper func(*http.Request) (*http.Response, error)

func (transport policyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type policyResponseBody struct {
	reads atomic.Int32
}

func (body *policyResponseBody) Read([]byte) (int, error) {
	body.reads.Add(1)
	return 0, io.EOF
}

func (*policyResponseBody) Close() error {
	return nil
}

func newPolicyTestFetcher(
	t *testing.T,
	transport policyRoundTripper,
) *RobotsAdmissionFetcher {
	t.Helper()
	inner := policyPageSource(func(
		_ context.Context,
		target *url.URL,
	) (pagefetch.FetchedPage, error) {
		return pagefetch.FetchedPage{URL: target}, nil
	})
	fetcher, err := NewRobotsAdmissionFetcher(
		inner,
		&http.Client{Transport: transport},
		"yago-crawler",
		8,
	)
	if err != nil {
		t.Fatalf("new robots fetcher: %v", err)
	}
	return fetcher
}

func policyResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func policyURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	target, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse policy URL: %v", err)
	}
	return target
}

func TestRobotsPolicyCacheSeparatesSchemesForSameHost(t *testing.T) {
	var requests atomic.Int32
	fetcher := newPolicyTestFetcher(t, func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.Scheme == "https" {
			return policyResponse(http.StatusOK, "User-agent: *\nDisallow: /private\n"), nil
		}
		return policyResponse(http.StatusNotFound, ""), nil
	})

	if _, err := fetcher.Fetch(
		context.Background(),
		policyURL(t, "http://example.com/private"),
	); err != nil {
		t.Fatalf("HTTP policy should allow: %v", err)
	}
	if _, err := fetcher.Fetch(
		context.Background(),
		policyURL(t, "https://example.com/private"),
	); !errors.Is(err, ErrDisallowed) {
		t.Fatalf("HTTPS policy error = %v, want ErrDisallowed", err)
	}
	if _, err := fetcher.Fetch(
		context.Background(),
		policyURL(t, "HTTP://EXAMPLE.COM/again"),
	); err != nil {
		t.Fatalf("equivalent HTTP origin should allow: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("robots requests = %d, want 2", got)
	}
}

func TestRobotsPolicyRefreshesAfterFreshnessWindow(t *testing.T) {
	var requests atomic.Int32
	fetcher := newPolicyTestFetcher(t, func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return policyResponse(http.StatusOK, "User-agent: *\nDisallow: /private\n"), nil
		}
		return policyResponse(http.StatusNotFound, ""), nil
	})
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	fetcher.policies.now = func() time.Time { return now }
	target := policyURL(t, "https://example.com/private")

	if _, err := fetcher.Fetch(context.Background(), target); !errors.Is(err, ErrDisallowed) {
		t.Fatalf("initial policy error = %v, want ErrDisallowed", err)
	}
	now = now.Add(robotsPolicyFreshness - time.Nanosecond)
	if _, err := fetcher.Fetch(context.Background(), target); !errors.Is(err, ErrDisallowed) {
		t.Fatalf("fresh policy error = %v, want ErrDisallowed", err)
	}
	now = now.Add(time.Nanosecond)
	if _, err := fetcher.Fetch(context.Background(), target); err != nil {
		t.Fatalf("refreshed policy should allow: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("robots requests = %d, want 2", got)
	}
}

func TestRobotsPolicySkipsRefreshWhenConcurrentLookupFilledCache(t *testing.T) {
	var requests atomic.Int32
	fetcher := newPolicyTestFetcher(t, func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return policyResponse(http.StatusNotFound, ""), nil
	})
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	fetcher.policies.now = func() time.Time { return now }
	target := policyURL(t, "https://example.com/page")
	origin := robotsOrigin(target)
	group := allowAll()
	fetcher.policies.entries.Add(origin, originPolicy{
		group:     group,
		expiresAt: now.Add(robotsPolicyFreshness),
	})

	resolved := fetcher.refreshOriginPolicy(context.Background(), target, origin)
	if resolved != group {
		t.Fatal("concurrent policy was not reused")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("robots requests = %d, want 0", got)
	}
}

func TestRobotsPolicyRetriesNetworkFailureWithoutRequestStorm(t *testing.T) {
	var requests atomic.Int32
	fetcher := newPolicyTestFetcher(t, func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return nil, errors.New("network unavailable")
		}
		return policyResponse(http.StatusNotFound, ""), nil
	})
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	fetcher.policies.now = func() time.Time { return now }
	target := policyURL(t, "https://example.com/page")

	for range 3 {
		if _, err := fetcher.Fetch(context.Background(), target); !errors.Is(err, ErrDisallowed) {
			t.Fatalf("unreachable policy error = %v, want ErrDisallowed", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("robots requests during retry window = %d, want 1", got)
	}
	now = now.Add(robotsRetryInterval)
	if _, err := fetcher.Fetch(context.Background(), target); err != nil {
		t.Fatalf("recovered policy should allow: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("robots requests after retry window = %d, want 2", got)
	}
}

func TestRobotsPolicyRetriesServerFailureWithoutReadingBody(t *testing.T) {
	var requests atomic.Int32
	body := &policyResponseBody{}
	fetcher := newPolicyTestFetcher(t, func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body}, nil
		}
		return policyResponse(http.StatusNotFound, ""), nil
	})
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	fetcher.policies.now = func() time.Time { return now }
	target := policyURL(t, "https://example.com/page")

	for range 3 {
		if _, err := fetcher.Fetch(context.Background(), target); !errors.Is(err, ErrDisallowed) {
			t.Fatalf("unreachable policy error = %v, want ErrDisallowed", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("robots requests during retry window = %d, want 1", got)
	}
	if got := body.reads.Load(); got != 0 {
		t.Fatalf("server failure body reads = %d, want 0", got)
	}
	now = now.Add(robotsRetryInterval)
	if _, err := fetcher.Fetch(context.Background(), target); err != nil {
		t.Fatalf("recovered policy should allow: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("robots requests after retry window = %d, want 2", got)
	}
}

func TestRobotsPolicyRetainsLastRulesDuringServerFailure(t *testing.T) {
	var requests atomic.Int32
	fetcher := newPolicyTestFetcher(t, func(*http.Request) (*http.Response, error) {
		switch requests.Add(1) {
		case 1:
			return policyResponse(http.StatusOK, "User-agent: *\nDisallow: /private\n"), nil
		case 2:
			return policyResponse(http.StatusServiceUnavailable, "temporary"), nil
		default:
			return policyResponse(http.StatusNotFound, ""), nil
		}
	})
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	fetcher.policies.now = func() time.Time { return now }
	target := policyURL(t, "https://example.com/private")

	if _, err := fetcher.Fetch(context.Background(), target); !errors.Is(err, ErrDisallowed) {
		t.Fatalf("initial policy error = %v, want ErrDisallowed", err)
	}
	now = now.Add(robotsPolicyFreshness)
	for range 2 {
		if _, err := fetcher.Fetch(context.Background(), target); !errors.Is(err, ErrDisallowed) {
			t.Fatalf("retained policy error = %v, want ErrDisallowed", err)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("robots requests during server failure = %d, want 2", got)
	}
	now = now.Add(robotsRetryInterval)
	if _, err := fetcher.Fetch(context.Background(), target); err != nil {
		t.Fatalf("recovered policy should allow: %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("robots requests after server recovery = %d, want 3", got)
	}
}

func TestRobotsPolicyCoalescesConcurrentOriginRefresh(t *testing.T) {
	var requests atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	fetcher := newPolicyTestFetcher(t, func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		return policyResponse(http.StatusNotFound, ""), nil
	})
	target := policyURL(t, "https://example.com/page")
	const callers = 64
	start := make(chan struct{})
	errorsByCaller := make(chan error, callers)
	var callersDone sync.WaitGroup
	callersDone.Add(callers)
	for range callers {
		go func() {
			defer callersDone.Done()
			<-start
			_, err := fetcher.Fetch(context.Background(), target)
			errorsByCaller <- err
		}()
	}
	close(start)
	<-started
	close(release)
	callersDone.Wait()
	close(errorsByCaller)

	for err := range errorsByCaller {
		if err != nil {
			t.Fatalf("concurrent fetch: %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("concurrent robots requests = %d, want 1", got)
	}
}
