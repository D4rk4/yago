package pipeline

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type admissionTestFrontier struct {
	job       crawljob.CrawlJob
	taken     atomic.Bool
	abandoned chan struct{}
}

func (frontier *admissionTestFrontier) Take(ctx context.Context) (crawljob.CrawlJob, bool) {
	if frontier.taken.CompareAndSwap(false, true) {
		return frontier.job, true
	}
	<-ctx.Done()

	return crawljob.CrawlJob{}, false
}

func (*admissionTestFrontier) Submit(
	context.Context,
	crawljob.CrawlJob,
	crawljob.DiscoveredLinks,
) uint64 {
	return 0
}

func (*admissionTestFrontier) Done(crawljob.CrawlJob, yagocrawlcontract.CrawlRunTally) {}

func (frontier *admissionTestFrontier) Abandon(crawljob.CrawlJob) {
	close(frontier.abandoned)
}

func (*admissionTestFrontier) ResolveRedirect(crawljob.CrawlJob, string) bool {
	return true
}

type canceledSuccessFeedback struct {
	successes atomic.Int32
}

func (*canceledSuccessFeedback) Throttled(string, time.Duration, time.Time) {}

func (feedback *canceledSuccessFeedback) Succeeded(string, time.Time) {
	feedback.successes.Add(1)
}

type canceledSuccessFrontier struct {
	outcomes atomic.Int32
}

func (*canceledSuccessFrontier) Take(context.Context) (crawljob.CrawlJob, bool) {
	return crawljob.CrawlJob{}, false
}

func (*canceledSuccessFrontier) Submit(
	context.Context,
	crawljob.CrawlJob,
	crawljob.DiscoveredLinks,
) uint64 {
	return 0
}

func (*canceledSuccessFrontier) Done(crawljob.CrawlJob, yagocrawlcontract.CrawlRunTally) {}

func (*canceledSuccessFrontier) Abandon(crawljob.CrawlJob) {}

func (*canceledSuccessFrontier) ResolveRedirect(crawljob.CrawlJob, string) bool {
	return true
}

func (frontier *canceledSuccessFrontier) RecordHostFetchOutcome(
	context.Context,
	crawljob.CrawlJob,
	bool,
) {
	frontier.outcomes.Add(1)
}

func TestLeaseBoundJobContextRejectsAlreadyLostGrant(t *testing.T) {
	grantContext, revoke := context.WithCancelCause(t.Context())
	revoke(crawllease.ErrLeaseLost)
	jobContext, release, granted := leaseBoundJobContext(
		t.Context(),
		grantContext,
		"lost-lease",
	)
	defer release()
	if granted || jobContext != nil {
		t.Fatalf("lost grant returned context %v and granted=%t", jobContext, granted)
	}
}

func TestWaitForLeaseAdmissionChangeObservesEveryWakeSource(t *testing.T) {
	if !waitForLeaseAdmissionChange(t.Context(), nil, nil) {
		t.Fatal("unrestricted admission did not proceed")
	}
	availability := make(chan struct{})
	close(availability)
	if !waitForLeaseAdmissionChange(t.Context(), availability, make(chan struct{})) {
		t.Fatal("availability change did not resume admission")
	}
	binding := make(chan struct{})
	close(binding)
	if !waitForLeaseAdmissionChange(t.Context(), make(chan struct{}), binding) {
		t.Fatal("binding change did not resume admission")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if waitForLeaseAdmissionChange(canceled, make(chan struct{}), make(chan struct{})) {
		t.Fatal("canceled admission wait proceeded")
	}
}

func TestRunStopsWhileWaitingForMissingLease(t *testing.T) {
	frontier := &admissionTestFrontier{
		job:       crawljob.CrawlJob{URL: "https://example.org/", LeaseID: "missing-lease"},
		abandoned: make(chan struct{}),
	}
	pipeline := &Pipeline{
		frontier:    frontier,
		leaseGrants: crawllease.NewGrantRegistry(t.Context(), 1),
	}
	acceptContext, cancelAccept := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		pipeline.run(acceptContext, t.Context())
		close(done)
	}()
	select {
	case <-frontier.abandoned:
	case <-time.After(time.Second):
		t.Fatal("missing lease job was not abandoned")
	}
	cancelAccept()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("admission wait did not stop after cancellation")
	}
}

func TestCanceledSuccessDoesNotHealHostState(t *testing.T) {
	feedback := &canceledSuccessFeedback{}
	frontier := &canceledSuccessFrontier{}
	pipeline := &Pipeline{frontier: frontier, loadFeedback: feedback}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	pipeline.recordHostFetchSuccess(canceled, crawljob.CrawlJob{URL: "https://example.org/"})
	if feedback.successes.Load() != 0 || frontier.outcomes.Load() != 0 {
		t.Fatalf(
			"canceled success changed feedback=%d outcomes=%d",
			feedback.successes.Load(),
			frontier.outcomes.Load(),
		)
	}
}
