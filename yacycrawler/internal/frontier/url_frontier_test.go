package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawladmission"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
)

func compiled(
	t *testing.T,
	profile yacycrawlcontract.CrawlProfile,
) crawladmission.AdmissionProfile {
	t.Helper()
	c, err := crawladmission.CompileProfile(yacycrawlcontract.NewCrawlProfile(profile))
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

func requestsFor(handle string, urls ...string) []yacycrawlcontract.CrawlRequest {
	reqs := make([]yacycrawlcontract.CrawlRequest, len(urls))
	for i, u := range urls {
		reqs[i] = yacycrawlcontract.CrawlRequest{URL: u, ProfileHandle: handle}
	}
	return reqs
}

func TestSeedRunDeduplicatesAndDelivers(t *testing.T) {
	f := frontier.NewFrontier(8)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
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

func TestSeedRunSkipsMismatchedProfileHandle(t *testing.T) {
	f := frontier.NewFrontier(8)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
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
	f := frontier.NewFrontier(8)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
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
	f := frontier.NewFrontier(8)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
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
	f := frontier.NewFrontier(8)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
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
	f.Submit(context.Background(), root, []string{"https://example.com/child"})
	child := receiveJob(t, f)
	if child.Depth != 1 {
		t.Errorf("child depth = %d, want 1", child.Depth)
	}
	f.Submit(context.Background(), child, []string{"https://example.com/grandchild"})
	f.Done(child)
	f.Done(root)
	select {
	case extra := <-f.Jobs():
		t.Errorf("did not expect job past max depth: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubmitForUnknownRunIsIgnored(t *testing.T) {
	f := frontier.NewFrontier(8)
	f.Submit(
		context.Background(),
		crawljob.CrawlJob{URL: "https://example.com/"},
		[]string{"https://example.com/x"},
	)
	select {
	case job := <-f.Jobs():
		t.Errorf("unknown run should produce no jobs, got %+v", job)
	case <-time.After(200 * time.Millisecond):
	}
}
