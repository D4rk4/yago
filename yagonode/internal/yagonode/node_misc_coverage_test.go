package yagonode

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/extractfetch"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

type failingOrderQueue struct{}

func (failingOrderQueue) PublishOnce(
	context.Context,
	string,
	yagocrawlcontract.CrawlOrder,
) (bool, error) {
	return false, errors.New("publish once failed")
}

func TestConfigViewToggleBranches(t *testing.T) {
	if got := yesNo(true); got != "Yes" {
		t.Fatalf("yesNo(true) = %q", got)
	}
	if got := yesNo(false); got != "No" {
		t.Fatalf("yesNo(false) = %q", got)
	}
	if got := enabledDisabled(true); got != "Enabled" {
		t.Fatalf("enabledDisabled(true) = %q", got)
	}
	if got := enabledDisabled(false); got != "Disabled" {
		t.Fatalf("enabledDisabled(false) = %q", got)
	}
}

func TestAdminSearchScopeAndTavilyDecision(t *testing.T) {
	if got := adminSearchScope(tavilyapi.ScopeRaw); got != adminauth.ScopeSearchRaw {
		t.Fatalf("raw scope = %v", got)
	}
	if got := adminSearchScope(tavilyapi.SearchScope(99)); got != adminauth.ScopeSearchRead {
		t.Fatalf("default scope = %v", got)
	}

	cases := map[adminauth.APIKeyOutcome]tavilyapi.AuthDecision{
		adminauth.APIKeyAuthorized:      tavilyapi.DecisionAllow,
		adminauth.APIKeyThrottled:       tavilyapi.DecisionThrottled,
		adminauth.APIKeyForbidden:       tavilyapi.DecisionForbidden,
		adminauth.APIKeyUnavailable:     tavilyapi.DecisionUnavailable,
		adminauth.APIKeyUnauthenticated: tavilyapi.DecisionUnauthenticated,
		adminauth.APIKeyOutcome(99):     tavilyapi.DecisionUnauthenticated,
	}
	for outcome, want := range cases {
		if got := tavilyDecision(outcome); got != want {
			t.Fatalf("tavilyDecision(%v) = %v, want %v", outcome, got, want)
		}
	}
}

func TestRequestHTTPSAndLoopbackBranches(t *testing.T) {
	ctx := context.Background()
	tlsReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://example.org/", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if !requestIsHTTPS(tlsReq) {
		t.Fatal("a TLS request must be reported as HTTPS")
	}

	hostPortReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://example.org/", nil)
	hostPortReq.Host = "127.0.0.1:8080"
	if !requestIsLoopback(hostPortReq) {
		t.Fatal("127.0.0.1:8080 must be loopback")
	}

	localhostReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://example.org/", nil)
	localhostReq.Host = "localhost"
	if !requestIsLoopback(localhostReq) {
		t.Fatal("localhost must be loopback")
	}
}

func TestSeedLastSeenPresent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	seed := yagomodel.Seed{
		LastSeen: yagomodel.Some(yagomodel.NewSeedLastSeenUTC(time.Unix(1_700_000_000, 0))),
	}
	if got := seedLastSeen(seed, now); got == "" {
		t.Fatal("a present LastSeen should render a timestamp")
	}
	if got := seedLastSeen(yagomodel.Seed{}, now); got != "" {
		t.Fatalf("an absent LastSeen should render empty, got %q", got)
	}
	future := yagomodel.Seed{
		LastSeen: yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now.Add(time.Hour))),
	}
	if got := seedLastSeen(future, now); got != "" {
		t.Fatalf("a future LastSeen should render empty, got %q", got)
	}
}

func TestBindingHelperEdgeBranches(t *testing.T) {
	base := testConfig(t)
	got := applyBindOverrides(base, map[string]string{
		bindKeyPeer:   "not-an-addr",
		"unknown.key": "127.0.0.1:80",
	})
	if got.PeerAddr != base.PeerAddr {
		t.Fatal("a malformed bind override must be ignored")
	}

	if _, err := parseBindPort("not-a-number"); err == nil {
		t.Fatal("a non-numeric port must error")
	}
	if _, err := parseBindPort("70000"); err == nil {
		t.Fatal("out-of-range port must error")
	}

	unspecified := func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(8, 32)},
			&net.IPAddr{IP: net.ParseIP("192.0.2.1")},
		}, nil
	}
	addresses, err := discoverBindAddresses(unspecified)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	for _, addr := range addresses {
		if addr.host == "0.0.0.0" {
			t.Fatal("the unspecified address should have been skipped")
		}
	}
}

func TestExtractContentFetcherWrapsError(t *testing.T) {
	fetcher := extractContentFetcher{
		fetcher: extractfetch.New(http.DefaultClient, time.Second, 1024),
	}
	if _, err := fetcher.Fetch(context.Background(), "://malformed"); err == nil {
		t.Fatal("a malformed URL must surface a fetch error")
	}
}

func TestWebCrawlSeederLogsPublishFailure(t *testing.T) {
	seeder := newWebCrawlSeeder(
		failingOrderQueue{},
		fakeSeedDocuments{},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{
			fallback: webFallbackConfig{SeedDepth: 0, SeedMaxPages: 1},
		},
	)
	seeder.Seed(context.Background(), []string{"https://unknown.example/"})
}

func TestProfileRecordingQueueSurfacesPublishError(t *testing.T) {
	queue := profileRecordingQueue{inner: failingOrderQueue{}, frontier: openTestFrontier(t)}
	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
			Name:            "Example",
			Scope:           yagocrawlcontract.ScopeDomain,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		}),
	}
	if _, err := queue.PublishOnce(context.Background(), "key", order); err == nil {
		t.Fatal("inner publish error must propagate")
	}
}
