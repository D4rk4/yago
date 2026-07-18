package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
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
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if job, ok := f.Take(ctx); ok {
		return job
	}
	t.Fatal("timed out waiting for job")

	return crawljob.CrawlJob{}
}

func assertNoJob(t *testing.T, f *frontier.Frontier, wait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	if job, ok := f.Take(ctx); ok {
		t.Fatalf("unexpected crawl job %q", job.URL)
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
		func(bool) { close(finished) },
	)
	if seeded.Queued != 2 {
		t.Fatalf("queued = %d, want 2", seeded.Queued)
	}
	f.Done(receiveJob(t, f), successfulPageOutcome())
	f.Done(receiveJob(t, f), successfulPageOutcome())
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("run finish callback never fired")
	}
}

func TestSeedRunPreservesSourceModificationDate(t *testing.T) {
	f := frontier.NewFrontier(1, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	modified := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	f.SeedRun(context.Background(), []yagocrawlcontract.CrawlRequest{{
		URL: "https://example.com/", ProfileHandle: profile.Profile.Handle,
		LastModified: modified,
	}}, []byte("admin"), profile, nil)
	job := receiveJob(t, f)
	if job.SourceModifiedAt != modified {
		t.Fatalf("source modification date = %v", job.SourceModifiedAt)
	}
}

func TestDoneDrainsRunWithQuarantinedPageFailure(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	succeeded := make(chan bool, 1)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		[]byte("admin"),
		profile,
		func(ok bool) { succeeded <- ok },
	)
	f.Done(receiveJob(t, f), failedPageOutcome())
	select {
	case ok := <-succeeded:
		if !ok {
			t.Error("a quarantined page failure must not requeue the whole run")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run finish callback never fired")
	}
}

type countingFrontierTally struct {
	provenance []byte
	tally      yagocrawlcontract.CrawlRunTally
}

func (c *countingFrontierTally) Commit(
	provenance []byte,
	tally yagocrawlcontract.CrawlRunTally,
) {
	c.provenance = append([]byte(nil), provenance...)
	c.tally.Duplicates += tally.Duplicates
}

func (c *countingFrontierTally) Snapshot([]byte) yagocrawlcontract.CrawlRunTally {
	return c.tally
}

func (c *countingFrontierTally) Restore(
	provenance []byte,
	tally yagocrawlcontract.CrawlRunTally,
) {
	c.provenance = append([]byte(nil), provenance...)
	c.tally = tally
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
		func(bool) {},
	)
	if seeded.Queued != 2 {
		t.Fatalf("queued = %d, want 2", seeded.Queued)
	}
	if tally.tally.Duplicates != 1 || string(tally.provenance) != "run-dup" {
		t.Fatalf("tally = %+v for %q, want one skip keyed run-dup", tally.tally, tally.provenance)
	}
	f.Done(receiveJob(t, f), successfulPageOutcome())
	f.Done(receiveJob(t, f), successfulPageOutcome())
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
		func(bool) { close(finished) },
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
		func(bool) {},
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
		func(bool) {},
	)
	if seeded.Queued != 1 {
		t.Fatalf("queued = %d, want 1 (host cap)", seeded.Queued)
	}
	f.Done(receiveJob(t, f), successfulPageOutcome())
}

// TestSeedRunHonoursPageBudget pins the whole-run page budget (PORT-03): a run
// stops admitting once it reaches its total page budget, above and beyond the
// per-host cap. With the per-host cap left unlimited, four URLs and a budget of
// two admit exactly two — the run budget, not the host cap, is what stops it —
// and the fourth admission exercises the already-warned path.
func TestSeedRunHonoursPageBudget(t *testing.T) {
	f := frontier.NewFrontier(8, nil, frontier.WithMaxPagesPerRun(2))
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
			"https://example.com/d",
		),
		nil,
		profile,
		func(bool) {},
	)
	if seeded.Queued != 2 {
		t.Fatalf("queued = %d, want 2 (per-run page budget)", seeded.Queued)
	}
	for range 2 {
		f.Done(receiveJob(t, f), successfulPageOutcome())
	}
}

func TestSeedRunUsesProfilePageBudget(t *testing.T) {
	f := frontier.NewFrontier(12, nil, frontier.WithMaxPagesPerRun(3))
	one := 1
	bounded := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		MaxPagesPerRun:  &one,
	})
	unlimited := 0
	unbounded := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		MaxPagesPerRun:  &unlimited,
	})

	boundedRun := f.SeedRun(
		context.Background(),
		requestsFor(bounded.Profile.Handle,
			"https://bounded.example/a",
			"https://bounded.example/b",
		),
		[]byte("bounded"),
		bounded,
		func(bool) {},
	)
	if boundedRun.Queued != 1 {
		t.Fatalf("bounded queued = %d, want 1", boundedRun.Queued)
	}

	unboundedRun := f.SeedRun(
		context.Background(),
		requestsFor(unbounded.Profile.Handle,
			"https://unbounded.example/a",
			"https://unbounded.example/b",
			"https://unbounded.example/c",
			"https://unbounded.example/d",
		),
		[]byte("unbounded"),
		unbounded,
		func(bool) {},
	)
	if unboundedRun.Queued != 4 {
		t.Fatalf("unbounded queued = %d, want 4", unboundedRun.Queued)
	}
	for range 5 {
		f.Done(receiveJob(t, f), successfulPageOutcome())
	}
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
		func(bool) {},
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
	f.Done(child, successfulPageOutcome())
	f.Done(root, successfulPageOutcome())
	assertNoJob(t, f, 200*time.Millisecond)
}

func TestSubmitForUnknownRunIsIgnored(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	f.Submit(
		context.Background(),
		crawljob.CrawlJob{URL: "https://example.com/"},
		discoveredLinks("https://example.com/x"),
	)
	assertNoJob(t, f, 200*time.Millisecond)
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
		func(bool) {},
	)
	root := receiveJob(t, f)
	root.ProfileHandle = "missing"

	f.Submit(context.Background(), root, discoveredLinks("https://example.com/child"))
	f.Done(crawljob.CrawlJob{RunID: seeded.RunID}, successfulPageOutcome())
	assertNoJob(t, f, 200*time.Millisecond)
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
		func(bool) {},
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
	assertNoJob(t, f, 200*time.Millisecond)
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
		func(bool) {},
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
		func(bool) {},
	)
	if seeded.Queued != 3 {
		t.Fatalf("queued = %d, want 3", seeded.Queued)
	}

	first := receiveJob(t, f)
	second := receiveJob(t, f)
	assertNoJob(t, f, 200*time.Millisecond)

	// Completing one in-flight fetch frees a host slot for the withheld job.
	f.Done(first, successfulPageOutcome())
	third := receiveJob(t, f)
	f.Done(second, successfulPageOutcome())
	f.Done(third, successfulPageOutcome())
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
		func(bool) {},
	)

	// A per-host cap of 1 must still let three distinct hosts run concurrently.
	first := receiveJob(t, f)
	second := receiveJob(t, f)
	third := receiveJob(t, f)
	f.Done(first, successfulPageOutcome())
	f.Done(second, successfulPageOutcome())
	f.Done(third, successfulPageOutcome())
}
