package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
)

type lostLeaseSuspensionFrontier struct {
	*leaseGrantWaitingFrontier
	suspended chan crawljob.CrawlJob
}

func (frontier *lostLeaseSuspensionFrontier) SuspendLostLeaseRun(
	job crawljob.CrawlJob,
) bool {
	frontier.suspended <- job

	return true
}

func TestPipelineSuspendsAnUngrantedRunWithoutParkingItsWorker(t *testing.T) {
	base := newLeaseGrantWaitingFrontier()
	frontier := &lostLeaseSuspensionFrontier{
		leaseGrantWaitingFrontier: base,
		suspended:                 make(chan crawljob.CrawlJob, 1),
	}
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	crawlerPipeline := pipeline.NewPipeline(
		frontier,
		fetchFunc(nil),
		pageindex.NewIndexBuilder(),
		&spyEmitter{},
		pipeline.WithLeaseGrants(registry),
	)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		crawlerPipeline.RunWorkers(ctx, ctx, 1)
		close(done)
	}()
	job := crawljob.CrawlJob{
		URL:        "https://example.org/lost-lease",
		Provenance: []byte("lost-lease-run"),
		LeaseID:    "lost-lease",
	}
	frontier.jobs <- job
	select {
	case suspended := <-frontier.suspended:
		if suspended.URL != job.URL || suspended.LeaseID != job.LeaseID {
			t.Fatalf("suspended job = %+v, want %+v", suspended, job)
		}
	case <-time.After(time.Second):
		t.Fatal("ungranted run was not suspended")
	}
	select {
	case abandoned := <-frontier.abandonEvents:
		t.Fatalf("suspended run was also abandoned through the fallback: %d", abandoned)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lost-lease worker did not stop")
	}
}
