package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

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

func runSeeds(
	profile crawladmission.AdmissionProfile,
	provenance []byte,
	requests []yacycrawlcontract.CrawlRequest,
) frontier.RunSeeds {
	return frontier.RunSeeds{
		RunID:      uuid.New(),
		Requests:   requests,
		Provenance: provenance,
		Profile:    profile,
	}
}

func TestSeedRunDeduplicatesAndDelivers(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	finished := make(chan struct{})
	queued, _ := f.SeedRun(
		context.Background(),
		runSeeds(profile, []byte("admin"), requestsFor(profile.Profile.Handle,
			"https://example.com/",
			"https://example.com/",
			"https://example.com/b",
		)),
		func(bool) { close(finished) },
	)
	if queued != 2 {
		t.Fatalf("queued = %d, want 2", queued)
	}
	f.Done(receiveJob(t, f), false)
	f.Done(receiveJob(t, f), false)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("run finish callback never fired")
	}
}

func TestSeedRunRejectsDuplicateRunID(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	runID := uuid.New()
	firstQueued, duplicate := f.SeedRun(
		context.Background(),
		frontier.RunSeeds{
			RunID:    runID,
			Requests: requestsFor(profile.Profile.Handle, "https://example.com/"),
			Profile:  profile,
		},
		func(bool) {},
	)
	if duplicate {
		t.Fatal("first seed reported as duplicate")
	}
	if firstQueued != 1 {
		t.Fatalf("first queued = %d, want 1", firstQueued)
	}

	secondQueued, duplicate := f.SeedRun(
		context.Background(),
		frontier.RunSeeds{
			RunID:    runID,
			Requests: requestsFor(profile.Profile.Handle, "https://example.com/other"),
			Profile:  profile,
		},
		func(bool) { t.Error("duplicate seed must not register a finish callback") },
	)
	if !duplicate {
		t.Fatal("re-seed with the same run id was not reported as duplicate")
	}
	if secondQueued != 0 {
		t.Fatalf("duplicate queued = %d, want 0", secondQueued)
	}

	f.Done(receiveJob(t, f), false)
	select {
	case extra := <-f.Jobs():
		t.Errorf("duplicate seed enqueued extra work: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSeedRunSkipsMismatchedProfileHandle(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	finished := make(chan struct{})
	queued, _ := f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor("wrong-handle", "https://example.com/")),
		func(bool) { close(finished) },
	)
	if queued != 0 {
		t.Fatalf("queued = %d, want 0", queued)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("empty run should finish immediately")
	}
}

func TestSeedRunRejectsUnparsableSeed(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	queued, _ := f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor(profile.Profile.Handle, "ftp://example.com/")),
		func(bool) {},
	)
	if queued != 0 {
		t.Fatalf("queued = %d, want 0", queued)
	}
}

func TestSeedRunHonoursHostCap(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: 1,
	})
	queued, _ := f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor(profile.Profile.Handle,
			"https://example.com/a",
			"https://example.com/b",
		)),
		func(bool) {},
	)
	if queued != 1 {
		t.Fatalf("queued = %d, want 1 (host cap)", queued)
	}
	f.Done(receiveJob(t, f), false)
}

func TestSubmitFollowsLinksWithinDepth(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	queued, _ := f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor(profile.Profile.Handle, "https://example.com/")),
		func(bool) {},
	)
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	root := receiveJob(t, f)
	f.Submit(context.Background(), root, []string{"https://example.com/child"})
	child := receiveJob(t, f)
	if child.Depth != 1 {
		t.Errorf("child depth = %d, want 1", child.Depth)
	}
	f.Submit(context.Background(), child, []string{"https://example.com/grandchild"})
	f.Done(child, false)
	f.Done(root, false)
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
		[]string{"https://example.com/x"},
	)
	select {
	case job := <-f.Jobs():
		t.Errorf("unknown run should produce no jobs, got %+v", job)
	case <-time.After(200 * time.Millisecond):
	}
}
