package frontier_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func conditionalFetchFor(entityTag string) *yagocrawlcontract.ConditionalFetch {
	return &yagocrawlcontract.ConditionalFetch{
		Validators: yagocrawlcontract.HTTPValidators{
			ETag:         entityTag,
			LastModified: "Fri, 30 Oct 2026 04:00:00 GMT",
		},
		Revision: yagocrawlcontract.DocumentRevision{
			ContentHash:          "content-hash",
			ExtractionGeneration: 5,
			FetchedAt:            time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC),
		},
	}
}

func requireConditionalFetch(
	t *testing.T,
	job crawljob.CrawlJob,
	want *yagocrawlcontract.ConditionalFetch,
) {
	t.Helper()
	got := job.ConditionalFetch
	if want == nil {
		if got != nil {
			t.Fatalf("job %s conditional fetch = %+v, want none", job.URL, got)
		}

		return
	}
	if got == nil || got.Validators != want.Validators || !got.Revision.Matches(want.Revision) {
		t.Fatalf("job %s conditional fetch = %+v, want %+v", job.URL, got, want)
	}
}

func receiveJobsByURL(
	t *testing.T,
	work *frontier.Frontier,
	count int,
) map[string]crawljob.CrawlJob {
	t.Helper()
	jobs := make(map[string]crawljob.CrawlJob, count)
	for range count {
		job := receiveJob(t, work)
		jobs[job.URL] = job
	}

	return jobs
}

func TestCheckpointedRunRestoresConditionalFetchAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	issued := conditionalFetchFor(`W/"v5-<&>"`)
	requests := []yagocrawlcontract.CrawlRequest{
		{
			URL:              "https://example.com/cached",
			ProfileHandle:    profile.Profile.Handle,
			ConditionalFetch: issued,
		},
		{URL: "https://example.com/uncached", ProfileHandle: profile.Profile.Handle},
	}
	seed := frontier.CrawlRunSeed{
		Requests:      requests,
		Provenance:    []byte("conditional-restart"),
		OrderIdentity: []byte("conditional-restart-order"),
	}
	firstCheckpoint := openRestartCheckpoint(t, path, "first")
	firstFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(firstCheckpoint))
	firstFrontier.SeedRunWithPriority(context.Background(), seed, profile, func(bool) {
		t.Error("crashed frontier unexpectedly settled")
	})
	claimed := receiveJobsByURL(t, firstFrontier, 2)
	requireConditionalFetch(t, claimed["https://example.com/cached"], issued)
	requireConditionalFetch(t, claimed["https://example.com/uncached"], nil)
	closeRestartCheckpoint(t, firstCheckpoint, "first")

	secondCheckpoint := openRestartCheckpoint(t, path, "recovered")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	secondFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(secondCheckpoint))
	seed.Requests = nil
	secondFrontier.SeedRunWithPriority(context.Background(), seed, profile, func(bool) {})
	restored := receiveJobsByURL(t, secondFrontier, 2)
	requireConditionalFetch(t, restored["https://example.com/cached"], issued)
	requireConditionalFetch(t, restored["https://example.com/uncached"], nil)
}

func TestInterruptedSeedAdmissionKeepsConditionalFetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	checkpoint := openRestartCheckpoint(t, path, "partial conditional seed")
	t.Cleanup(func() { _ = checkpoint.Close() })
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	_, existingPage, newPage := interruptedSeedPages(profile.Profile.Handle)
	existingPage.ConditionalFetch = conditionalFetchFor(`"existing"`)
	newPage.ConditionalFetch = conditionalFetchFor(`"new"`)
	seed := frontier.CrawlRunSeed{
		Provenance:    []byte("partial-conditional-seed"),
		OrderIdentity: []byte("partial-conditional-order"),
	}
	persistInterruptedSeedPrefix(
		t,
		checkpoint,
		seed.Provenance,
		seed.OrderIdentity,
		[]frontiercheckpoint.Page{existingPage, newPage},
	)
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(checkpoint))
	crawlFrontier.SeedRunWithPriority(context.Background(), seed, profile, func(bool) {})

	jobs := receiveJobsByURL(t, crawlFrontier, 2)
	requireConditionalFetch(t, jobs[existingPage.URL], existingPage.ConditionalFetch)
	requireConditionalFetch(t, jobs[newPage.URL], newPage.ConditionalFetch)
}

func TestDiscoveredLinksDoNotInheritConditionalFetch(t *testing.T) {
	work := frontier.NewFrontier(4, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	issued := conditionalFetchFor(`"parent"`)
	work.SeedRunWithPriority(context.Background(), frontier.CrawlRunSeed{
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:              "https://example.com/parent",
			ProfileHandle:    profile.Profile.Handle,
			ConditionalFetch: issued,
		}},
		Provenance:    []byte("conditional-parent"),
		OrderIdentity: []byte("conditional-parent-order"),
	}, profile, func(bool) {})
	parent := receiveJob(t, work)
	requireConditionalFetch(t, parent, issued)

	work.Submit(context.Background(), parent, discoveredLinks("https://example.com/child"))

	requireConditionalFetch(t, receiveJob(t, work), nil)
}
