package frontier_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type delayedRunCheckpoint struct {
	frontier.Checkpoint
	provenance []byte
	started    chan struct{}
	release    chan struct{}
	once       sync.Once
}

type delayedCompletionCheckpoint struct {
	frontier.Checkpoint
	committed      chan struct{}
	release        chan struct{}
	controlUpdates atomic.Int64
}

func (checkpoint *delayedCompletionCheckpoint) CompletePage(
	ctx context.Context,
	provenance []byte,
	pageURL string,
	completion frontiercheckpoint.PageCompletion,
) error {
	if err := checkpoint.Checkpoint.CompletePage(
		ctx,
		provenance,
		pageURL,
		completion,
	); err != nil {
		return fmt.Errorf("complete delayed frontier page: %w", err)
	}
	close(checkpoint.committed)
	select {
	case <-checkpoint.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for delayed completion release: %w", ctx.Err())
	}
}

func (checkpoint *delayedCompletionCheckpoint) UpdateControl(
	ctx context.Context,
	provenance []byte,
	update frontiercheckpoint.ControlUpdate,
) error {
	checkpoint.controlUpdates.Add(1)

	if err := checkpoint.Checkpoint.UpdateControl(ctx, provenance, update); err != nil {
		return fmt.Errorf("update delayed frontier control: %w", err)
	}

	return nil
}

func (checkpoint *delayedRunCheckpoint) AdmitSeedBatch(
	ctx context.Context,
	provenance []byte,
	batch frontiercheckpoint.SeedBatch,
) (frontiercheckpoint.SeedBatchResult, error) {
	if bytes.Equal(provenance, checkpoint.provenance) {
		checkpoint.once.Do(func() { close(checkpoint.started) })
		select {
		case <-checkpoint.release:
		case <-ctx.Done():
			return frontiercheckpoint.SeedBatchResult{}, fmt.Errorf(
				"wait for delayed seed batch release: %w",
				ctx.Err(),
			)
		}
	}

	result, err := checkpoint.Checkpoint.AdmitSeedBatch(ctx, provenance, batch)
	if err != nil {
		return frontiercheckpoint.SeedBatchResult{}, fmt.Errorf(
			"admit delayed frontier seed batch: %w",
			err,
		)
	}

	return result, nil
}

func TestDelayedCheckpointRunDoesNotBlockAnotherRun(t *testing.T) {
	stored, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "frontier-v1.db"))
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = stored.Close() })
	firstProvenance := []byte("delayed-run")
	checkpoint := &delayedRunCheckpoint{
		Checkpoint: stored,
		provenance: firstProvenance,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(checkpoint))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	firstSeeded := make(chan frontier.SeededRun, 1)
	go func() {
		firstSeeded <- crawlFrontier.SeedRunWithPriority(
			context.Background(),
			frontier.CrawlRunSeed{
				Requests:      requestsFor(profile.Profile.Handle, "https://delayed.example/page"),
				Provenance:    firstProvenance,
				OrderIdentity: []byte("delayed-identity"),
			},
			profile,
			func(bool) {},
		)
	}()
	select {
	case <-checkpoint.started:
	case <-time.After(time.Second):
		t.Fatal("delayed checkpoint admission did not start")
	}
	secondSeeded := make(chan frontier.SeededRun, 1)
	go func() {
		secondSeeded <- crawlFrontier.SeedRunWithPriority(
			context.Background(),
			frontier.CrawlRunSeed{
				Requests:      requestsFor(profile.Profile.Handle, "https://ready.example/page"),
				Provenance:    []byte("ready-run"),
				OrderIdentity: []byte("ready-identity"),
			},
			profile,
			func(bool) {},
		)
	}()
	select {
	case seeded := <-secondSeeded:
		if seeded.Queued != 1 {
			t.Fatalf("second queued = %d, want 1", seeded.Queued)
		}
	case <-time.After(time.Second):
		t.Fatal("unrelated run waited for delayed checkpoint admission")
	}
	job := receiveJob(t, crawlFrontier)
	if string(job.Provenance) != "ready-run" {
		t.Fatalf("dispatch provenance = %q, want ready-run", job.Provenance)
	}
	crawlFrontier.Done(job, successfulPageOutcome())
	close(checkpoint.release)
	select {
	case seeded := <-firstSeeded:
		if seeded.Queued != 1 {
			t.Fatalf("first queued = %d, want 1", seeded.Queued)
		}
	case <-time.After(time.Second):
		t.Fatal("delayed run did not resume")
	}
}

func TestFinalPageCompletionSerializesLateControl(t *testing.T) {
	stored, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "frontier-v1.db"))
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = stored.Close() })
	checkpoint := &delayedCompletionCheckpoint{
		Checkpoint: stored,
		committed:  make(chan struct{}),
		release:    make(chan struct{}),
	}
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("final-page-control")
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      requestsFor(profile.Profile.Handle, "https://example.com/final"),
			Provenance:    provenance,
			OrderIdentity: []byte("final-page-identity"),
		},
		profile,
		func(bool) {},
	)
	job := receiveJob(t, crawlFrontier)
	completed := make(chan struct{})
	go func() {
		crawlFrontier.Done(job, successfulPageOutcome())
		close(completed)
	}()
	select {
	case <-checkpoint.committed:
	case <-time.After(time.Second):
		t.Fatal("final checkpoint completion did not commit")
	}
	controlled := make(chan struct{})
	go func() {
		crawlFrontier.Pause(provenance)
		close(controlled)
	}()
	select {
	case <-controlled:
		t.Fatal("late control bypassed final-page durability")
	case <-time.After(20 * time.Millisecond):
	}
	close(checkpoint.release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("final page completion did not return")
	}
	select {
	case <-controlled:
	case <-time.After(time.Second):
		t.Fatal("late control did not return after completion")
	}
	if got := checkpoint.controlUpdates.Load(); got != 0 {
		t.Fatalf("late checkpoint control updates = %d, want 0", got)
	}
	if err := crawlFrontier.CheckpointFailure(); err != nil {
		t.Fatalf("late control checkpoint failure: %v", err)
	}
}
