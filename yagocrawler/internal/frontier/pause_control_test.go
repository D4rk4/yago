package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

func TestPauseWithholdsRunJobsUntilResume(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("run-1")

	f.Pause(provenance)
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		provenance,
		profile,
		func() {},
	)
	if seeded.Queued != 1 {
		t.Fatalf("queued = %d, want 1", seeded.Queued)
	}

	select {
	case job := <-f.Jobs():
		t.Fatalf("paused run dispatched %q, want nothing", job.URL)
	case <-time.After(200 * time.Millisecond):
	}

	f.Resume(provenance)
	job := receiveJob(t, f)
	if job.URL != "https://example.com/" {
		t.Fatalf("resumed job = %q, want the seed URL", job.URL)
	}
	f.Done(job)
}

func TestPauseIsScopedToOneRun(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})

	f.Pause([]byte("paused-run"))
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://paused.example/"),
		[]byte("paused-run"),
		profile,
		func() {},
	)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://live.example/"),
		[]byte("live-run"),
		profile,
		func() {},
	)

	// The un-paused run's job dispatches even while the other run is withheld.
	job := receiveJob(t, f)
	if job.URL != "https://live.example/" {
		t.Fatalf("dispatched %q, want the live run's job", job.URL)
	}
	f.Done(job)
}
