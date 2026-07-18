package pipeline_test

import (
	"context"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type leaseGrantWaitingFrontier struct {
	jobs          chan crawljob.CrawlJob
	takeEvents    chan int32
	abandonEvents chan int32
	takes         atomic.Int32
	abandons      atomic.Int32
	bindingMutex  sync.Mutex
	bindingEvents chan struct{}
}

func newLeaseGrantWaitingFrontier() *leaseGrantWaitingFrontier {
	return &leaseGrantWaitingFrontier{
		jobs:          make(chan crawljob.CrawlJob, 1),
		takeEvents:    make(chan int32, 8),
		abandonEvents: make(chan int32, 8),
		bindingEvents: make(chan struct{}),
	}
}

func (frontier *leaseGrantWaitingFrontier) Take(
	ctx context.Context,
) (crawljob.CrawlJob, bool) {
	if ctx.Err() != nil {
		return crawljob.CrawlJob{}, false
	}
	select {
	case job := <-frontier.jobs:
		attempt := frontier.takes.Add(1)
		frontier.takeEvents <- attempt

		return job, true
	case <-ctx.Done():
		return crawljob.CrawlJob{}, false
	}
}

func (*leaseGrantWaitingFrontier) Submit(
	context.Context,
	crawljob.CrawlJob,
	crawljob.DiscoveredLinks,
) uint64 {
	return 0
}

func (*leaseGrantWaitingFrontier) Done(
	crawljob.CrawlJob,
	yagocrawlcontract.CrawlRunTally,
) {
}

func (frontier *leaseGrantWaitingFrontier) Abandon(job crawljob.CrawlJob) {
	attempt := frontier.abandons.Add(1)
	frontier.abandonEvents <- attempt
	frontier.jobs <- job
}

func (*leaseGrantWaitingFrontier) ResolveRedirect(crawljob.CrawlJob, string) bool {
	return true
}

func (frontier *leaseGrantWaitingFrontier) LeaseBindingChanges() <-chan struct{} {
	frontier.bindingMutex.Lock()
	defer frontier.bindingMutex.Unlock()

	return frontier.bindingEvents
}

func (frontier *leaseGrantWaitingFrontier) rebindQueuedLease(leaseID string) {
	job := <-frontier.jobs
	job.LeaseID = leaseID
	frontier.jobs <- job
	frontier.bindingMutex.Lock()
	close(frontier.bindingEvents)
	frontier.bindingEvents = make(chan struct{})
	frontier.bindingMutex.Unlock()
}

func TestPipelineParksUngrantedLeaseUntilRegistryAvailabilityChanges(t *testing.T) {
	frontier := newLeaseGrantWaitingFrontier()
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	fetchStarted := make(chan struct{})
	crawlerPipeline := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(ctx context.Context, _ *url.URL) (pagefetch.FetchedPage, error) {
			close(fetchStarted)
			<-ctx.Done()

			return pagefetch.FetchedPage{}, ctx.Err()
		}),
		pageindex.NewIndexBuilder(),
		&spyEmitter{},
		pipeline.WithLeaseGrants(registry),
	)
	acceptCtx, cancelAccept := context.WithCancel(t.Context())
	fetchCtx, cancelFetch := context.WithCancel(t.Context())
	workerDone := make(chan struct{})
	go func() {
		crawlerPipeline.RunWorkers(acceptCtx, fetchCtx, 1)
		close(workerDone)
	}()
	job := crawljob.CrawlJob{
		URL:           "https://example.org/lease-wait",
		ProfileHandle: "lease-wait",
		LeaseID:       "replacement-lease",
	}
	frontier.jobs <- job
	awaitAdmissionEvent(t, frontier.takeEvents, 1)
	awaitAdmissionEvent(t, frontier.abandonEvents, 1)
	rejectAdmissionEvent(t, frontier.takeEvents)
	if err := registry.Track(job.LeaseID); err != nil {
		t.Fatalf("track replacement lease: %v", err)
	}
	awaitAdmissionEvent(t, frontier.takeEvents, 2)
	awaitAdmissionEvent(t, frontier.abandonEvents, 2)
	rejectAdmissionEvent(t, frontier.takeEvents)
	started := time.Now()
	registry.Renew(
		started,
		time.Hour,
		[]string{job.LeaseID},
		[]string{job.LeaseID},
	)
	awaitAdmissionEvent(t, frontier.takeEvents, 3)
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("confirmed replacement lease did not resume fetching")
	}
	if takes, abandons := frontier.takes.Load(), frontier.abandons.Load(); takes != 3 ||
		abandons != 2 {
		t.Fatalf("lease admission attempts/abandons = %d/%d", takes, abandons)
	}
	cancelAccept()
	cancelFetch()
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("lease-wait worker did not stop")
	}
}

func TestPipelineResumesWhenFrontierBindsAlreadyConfirmedReplacementLease(t *testing.T) {
	frontier := newLeaseGrantWaitingFrontier()
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	replacementLeaseID := "already-confirmed-replacement"
	if err := registry.Track(replacementLeaseID); err != nil {
		t.Fatalf("track already confirmed replacement lease: %v", err)
	}
	started := time.Now()
	registry.Renew(
		started,
		time.Hour,
		[]string{replacementLeaseID},
		[]string{replacementLeaseID},
	)
	fetchStarted := make(chan struct{})
	crawlerPipeline := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(ctx context.Context, _ *url.URL) (pagefetch.FetchedPage, error) {
			close(fetchStarted)
			<-ctx.Done()

			return pagefetch.FetchedPage{}, ctx.Err()
		}),
		pageindex.NewIndexBuilder(),
		&spyEmitter{},
		pipeline.WithLeaseGrants(registry),
	)
	acceptCtx, cancelAccept := context.WithCancel(t.Context())
	fetchCtx, cancelFetch := context.WithCancel(t.Context())
	workerDone := make(chan struct{})
	go func() {
		crawlerPipeline.RunWorkers(acceptCtx, fetchCtx, 1)
		close(workerDone)
	}()
	frontier.jobs <- crawljob.CrawlJob{
		URL:           "https://example.org/lease-rebind-wait",
		ProfileHandle: "lease-rebind-wait",
		LeaseID:       "expired-lease",
	}
	awaitAdmissionEvent(t, frontier.takeEvents, 1)
	awaitAdmissionEvent(t, frontier.abandonEvents, 1)
	rejectAdmissionEvent(t, frontier.takeEvents)
	frontier.rebindQueuedLease(replacementLeaseID)
	awaitAdmissionEvent(t, frontier.takeEvents, 2)
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("frontier lease rebind did not resume fetching")
	}
	if takes, abandons := frontier.takes.Load(), frontier.abandons.Load(); takes != 2 ||
		abandons != 1 {
		t.Fatalf("lease rebind attempts/abandons = %d/%d", takes, abandons)
	}
	cancelAccept()
	cancelFetch()
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("lease-rebind wait worker did not stop")
	}
}

func awaitAdmissionEvent(t *testing.T, events <-chan int32, want int32) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("admission event = %d, want %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("admission event %d did not arrive", want)
	}
}

func rejectAdmissionEvent(t *testing.T, events <-chan int32) {
	t.Helper()
	select {
	case got := <-events:
		t.Fatalf("unnotified admission attempt = %d", got)
	case <-time.After(30 * time.Millisecond):
	}
}
