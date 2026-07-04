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
		func() { close(finished) },
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
	select {
	case job := <-f.Jobs():
		t.Fatalf("cancelled run dispatched %q, want nothing", job.URL)
	case <-time.After(100 * time.Millisecond):
	}

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
		func() {},
	)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://survivor.example/"),
		[]byte("survivor"),
		profile,
		func() {},
	)

	f.Cancel([]byte("doomed"))

	f.Resume([]byte("survivor"))
	job := receiveJob(t, f)
	if job.URL != "https://survivor.example/" {
		t.Fatalf("dispatched %q, want the survivor run's job", job.URL)
	}
	f.Done(job)
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
		func() {},
	)

	work := receiveJob(t, f)
	f.Cancel(work.Provenance)
	f.Submit(context.Background(), work, discoveredLinks("https://example.com/child"))
	f.Done(work)

	select {
	case job := <-f.Jobs():
		t.Fatalf("cancelled run dispatched discovered link %q, want nothing", job.URL)
	case <-time.After(100 * time.Millisecond):
	}
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
		func() {},
	)

	first := receiveJob(t, f)
	f.Done(first)

	select {
	case job := <-f.Jobs():
		t.Fatalf("throttled run dispatched %q too soon", job.URL)
	case <-time.After(200 * time.Millisecond):
	}

	// Lifting the throttle releases the withheld job at once.
	f.SetRate(provenance, 0)
	second := receiveJob(t, f)
	f.Done(second)
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
