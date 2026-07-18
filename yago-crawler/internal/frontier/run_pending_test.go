package frontier_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRunPendingReflectsOutstandingJobs(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("run-pending")

	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/a", "https://example.com/b"),
		provenance,
		profile,
		func(bool) {},
	)
	if got := f.RunPending(seeded.RunID); got != 2 {
		t.Fatalf("RunPending after seeding = %d, want 2", got)
	}

	job := receiveJob(t, f)
	f.Done(job, successfulPageOutcome())
	if got := f.RunPending(seeded.RunID); got != 1 {
		t.Fatalf("RunPending after one Done = %d, want 1", got)
	}

	job = receiveJob(t, f)
	f.Done(job, successfulPageOutcome())
	if got := f.RunPending(seeded.RunID); got != 0 {
		t.Fatalf("RunPending after draining = %d, want 0", got)
	}
}
