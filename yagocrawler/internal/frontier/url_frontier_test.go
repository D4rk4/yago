package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

func compiled(
	t *testing.T,
	profile yagocrawlcontract.CrawlProfile,
) crawladmission.AdmissionProfile {
	t.Helper()
	c, err := crawladmission.CompileProfile(yagocrawlcontract.NewCrawlProfile(profile))
	if err != nil {
		t.Fatalf("compile profile: %v", err)
	}
	return c
}

func receiveJob(t *testing.T, f *frontier.Frontier) crawljob.CrawlJob {
	t.Helper()
	select {
	case job := <-f.Jobs():
		return job
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for job")
		return crawljob.CrawlJob{}
	}
}

func requestsFor(handle string, urls ...string) []yagocrawlcontract.CrawlRequest {
	reqs := make([]yagocrawlcontract.CrawlRequest, len(urls))
	for i, u := range urls {
		reqs[i] = yagocrawlcontract.CrawlRequest{URL: u, ProfileHandle: handle}
	}
	return reqs
}

func discoveredLinks(urls ...string) crawljob.DiscoveredLinks {
	return crawljob.DiscoveredLinks{Followable: urls}
}

func TestSeedRunDeduplicatesAndDelivers(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	finished := make(chan struct{})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://example.com/",
			"https://example.com/",
			"https://example.com/b",
		),
		[]byte("admin"),
		profile,
		func() { close(finished) },
	)
	if seeded.Queued != 2 {
		t.Fatalf("queued = %d, want 2", seeded.Queued)
	}
	f.Done(receiveJob(t, f))
	f.Done(receiveJob(t, f))
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("run finish callback never fired")
	}
}

type countingFrontierTally struct {
	duplicates [][]byte
}

func (c *countingFrontierTally) Duplicate(provenance []byte) {
	c.duplicates = append(c.duplicates, append([]byte(nil), provenance...))
}

func TestSeedRunCountsDuplicateSkips(t *testing.T) {
	tally := &countingFrontierTally{}
	f := frontier.NewFrontier(8, nil, frontier.WithRunTally(tally))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://example.com/",
			"https://example.com/",
			"https://example.com/b",
		),
		[]byte("run-dup"),
		profile,
		func() {},
	)
	if seeded.Queued != 2 {
		t.Fatalf("queued = %d, want 2", seeded.Queued)
	}
	if len(tally.duplicates) != 1 || string(tally.duplicates[0]) != "run-dup" {
		t.Fatalf("duplicates = %v, want one skip keyed run-dup", tally.duplicates)
	}
	f.Done(receiveJob(t, f))
	f.Done(receiveJob(t, f))
}

func TestSeedRunSkipsMismatchedProfileHandle(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	finished := make(chan struct{})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor("wrong-handle", "https://example.com/"),
		nil,
		profile,
		func() { close(finished) },
	)
	if seeded.Queued != 0 {
		t.Fatalf("queued = %d, want 0", seeded.Queued)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("empty run should finish immediately")
	}
}

func TestSeedRunRejectsUnparsableSeed(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "ftp://example.com/"),
		nil,
		profile,
		func() {},
	)
	if seeded.Queued != 0 {
		t.Fatalf("queued = %d, want 0", seeded.Queued)
	}
}

func TestSeedRunHonoursHostCap(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: 1,
	})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://example.com/a",
			"https://example.com/b",
		),
		nil,
		profile,
		func() {},
	)
	if seeded.Queued != 1 {
		t.Fatalf("queued = %d, want 1 (host cap)", seeded.Queued)
	}
	f.Done(receiveJob(t, f))
}

func TestSubmitFollowsLinksWithinDepth(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		func() {},
	)
	if seeded.Queued != 1 {
		t.Fatalf("queued = %d, want 1", seeded.Queued)
	}
	root := receiveJob(t, f)
	f.Submit(context.Background(), root, discoveredLinks("https://example.com/child"))
	child := receiveJob(t, f)
	if child.Depth != 1 {
		t.Errorf("child depth = %d, want 1", child.Depth)
	}
	f.Submit(context.Background(), child, discoveredLinks("https://example.com/grandchild"))
	f.Done(child)
	f.Done(root)
	select {
	case extra := <-f.Jobs():
		t.Errorf("did not expect job past max depth: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubmitForUnknownRunIsIgnored(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	f.Submit(
		context.Background(),
		crawljob.CrawlJob{URL: "https://example.com/"},
		discoveredLinks("https://example.com/x"),
	)
	select {
	case job := <-f.Jobs():
		t.Errorf("unknown run should produce no jobs, got %+v", job)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubmitForUnknownProfileIsIgnored(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		func() {},
	)
	root := receiveJob(t, f)
	root.ProfileHandle = "missing"

	f.Submit(context.Background(), root, discoveredLinks("https://example.com/child"))
	f.Done(crawljob.CrawlJob{RunID: seeded.RunID})
	select {
	case job := <-f.Jobs():
		t.Errorf("unknown profile should produce no jobs, got %+v", job)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubmitSkipsNoFollowLinksByDefault(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		func() {},
	)
	root := receiveJob(t, f)
	f.Submit(context.Background(), root, crawljob.DiscoveredLinks{
		Followable: []string{"https://example.com/child"},
		NoFollow:   []string{"https://example.com/blocked"},
	})
	child := receiveJob(t, f)
	if child.URL != "https://example.com/child" {
		t.Fatalf("child URL = %q", child.URL)
	}
	select {
	case extra := <-f.Jobs():
		t.Fatalf("nofollow link should not be queued by default: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubmitFollowsNoFollowLinksWhenProfileAllows(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:               yagocrawlcontract.ScopeDomain,
		URLMustMatch:        yagocrawlcontract.MatchAll,
		MaxDepth:            1,
		FollowNoFollowLinks: true,
		MaxPagesPerHost:     yagocrawlcontract.UnlimitedPagesPerHost,
	})
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		func() {},
	)
	root := receiveJob(t, f)
	f.Submit(context.Background(), root, crawljob.DiscoveredLinks{
		NoFollow: []string{"https://example.com/blocked"},
	})
	child := receiveJob(t, f)
	if child.URL != "https://example.com/blocked" {
		t.Fatalf("child URL = %q", child.URL)
	}
}

func TestFrontierBoundsPerHostConcurrency(t *testing.T) {
	f := frontier.NewFrontier(8, nil, frontier.WithMaxHostConcurrency(2))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://example.com/a",
			"https://example.com/b",
			"https://example.com/c",
		),
		[]byte("admin"),
		profile,
		func() {},
	)
	if seeded.Queued != 3 {
		t.Fatalf("queued = %d, want 3", seeded.Queued)
	}

	first := receiveJob(t, f)
	second := receiveJob(t, f)
	select {
	case extra := <-f.Jobs():
		t.Fatalf("dispatched %s while host at concurrency cap", extra.URL)
	case <-time.After(200 * time.Millisecond):
	}

	// Completing one in-flight fetch frees a host slot for the withheld job.
	f.Done(first)
	third := receiveJob(t, f)
	f.Done(second)
	f.Done(third)
}

func TestFrontierAllowsConcurrencyAcrossHosts(t *testing.T) {
	f := frontier.NewFrontier(8, nil, frontier.WithMaxHostConcurrency(1))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://a.example/",
			"https://b.example/",
			"https://c.example/",
		),
		[]byte("admin"),
		profile,
		func() {},
	)

	// A per-host cap of 1 must still let three distinct hosts run concurrently.
	first := receiveJob(t, f)
	second := receiveJob(t, f)
	third := receiveJob(t, f)
	f.Done(first)
	f.Done(second)
	f.Done(third)
}
