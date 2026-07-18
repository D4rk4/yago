package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
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
		func(bool) {},
	)
	if seeded.Queued != 1 {
		t.Fatalf("queued = %d, want 1", seeded.Queued)
	}

	assertNoJob(t, f, 200*time.Millisecond)

	f.Resume(provenance)
	job := receiveJob(t, f)
	if job.URL != "https://example.com/" {
		t.Fatalf("resumed job = %q, want the seed URL", job.URL)
	}
	f.Done(job, successfulPageOutcome())
}

func TestCancelDropsPendingJobsAndDrainsRun(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("run-x")
	finished := make(chan struct{})

	// Pausing holds the seed job in the ready queue so Cancel has a pending job to
	// drop, exercising the drop-and-settle path.
	f.Pause(provenance)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		provenance,
		profile,
		func(bool) { close(finished) },
	)

	f.Cancel(provenance)

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled run never drained")
	}
	if !f.WasCancelled(provenance) {
		t.Fatal("WasCancelled = false after Cancel")
	}
	assertNoJob(t, f, 100*time.Millisecond)

	f.ClearCancelled(provenance)
	if f.WasCancelled(provenance) {
		t.Fatal("WasCancelled = true after ClearCancelled")
	}
}

func TestCancelKeepsOtherRunsPendingJobs(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})

	// Both runs are paused so their seed jobs sit in the ready queue when Cancel
	// scans it — the survivor's job must be kept, not dropped alongside the doomed run.
	f.Pause([]byte("doomed"))
	f.Pause([]byte("survivor"))
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://doomed.example/"),
		[]byte("doomed"),
		profile,
		func(bool) {},
	)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://survivor.example/"),
		[]byte("survivor"),
		profile,
		func(bool) {},
	)

	f.Cancel([]byte("doomed"))

	f.Resume([]byte("survivor"))
	job := receiveJob(t, f)
	if job.URL != "https://survivor.example/" {
		t.Fatalf("dispatched %q, want the survivor run's job", job.URL)
	}
	f.Done(job, successfulPageOutcome())
}

func TestCancelRejectsDiscoveredLinks(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        2,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("run-z")
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		provenance,
		profile,
		func(bool) {},
	)

	work := receiveJob(t, f)
	f.Cancel(work.Provenance)
	f.Submit(context.Background(), work, discoveredLinks("https://example.com/child"))
	f.Done(work, successfulPageOutcome())

	assertNoJob(t, f, 100*time.Millisecond)
}

// TestDefaultRunRateThrottlesFromFirstJob: with a frontier-wide default rate,
// a freshly seeded run paces immediately — no operator SetRate needed — and an
// explicit SetRate of zero deliberately unleashes it past the default.
func TestDefaultRunRateThrottlesFromFirstJob(t *testing.T) {
	f := frontier.NewFrontier(8, nil, frontier.WithDefaultRunRate(1))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("default-rated-run")

	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://a.example/", "https://b.example/"),
		provenance,
		profile,
		func(bool) {},
	)

	first := receiveJob(t, f)
	f.Done(first, successfulPageOutcome())

	// One page per minute spaces dispatches 60s apart, so the second job stays
	// withheld well beyond the test window without any explicit SetRate.
	assertNoJob(t, f, 200*time.Millisecond)

	// An explicit zero rate overrides the default and unleashes the run.
	f.SetRate(provenance, 0)
	second := receiveJob(t, f)
	f.Done(second, successfulPageOutcome())
}

func TestSetRateThrottlesRunDispatch(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("rated-run")

	// One page per minute spaces dispatches 60s apart, so after the first job the
	// second is withheld well beyond the test window.
	f.SetRate(provenance, 1)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://a.example/", "https://b.example/"),
		provenance,
		profile,
		func(bool) {},
	)

	first := receiveJob(t, f)
	f.Done(first, successfulPageOutcome())

	assertNoJob(t, f, 200*time.Millisecond)

	// Lifting the throttle releases the withheld job at once.
	f.SetRate(provenance, 0)
	second := receiveJob(t, f)
	f.Done(second, successfulPageOutcome())
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
		func(bool) {},
	)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://live.example/"),
		[]byte("live-run"),
		profile,
		func(bool) {},
	)

	// The un-paused run's job dispatches even while the other run is withheld.
	job := receiveJob(t, f)
	if job.URL != "https://live.example/" {
		t.Fatalf("dispatched %q, want the live run's job", job.URL)
	}
	f.Done(job, successfulPageOutcome())
}
