package frontier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type deferredPageRecordingCheckpoint struct {
	Checkpoint
	err     error
	deferFn func()
	pageURL string
	attempt uint8
	retryAt time.Time
	ctxErr  error
}

func (checkpoint *deferredPageRecordingCheckpoint) DeferPage(
	ctx context.Context,
	_ []byte,
	pageURL string,
	attempt uint8,
	retryAt time.Time,
) error {
	checkpoint.pageURL = pageURL
	checkpoint.attempt = attempt
	checkpoint.retryAt = retryAt
	checkpoint.ctxErr = ctx.Err()
	if checkpoint.deferFn != nil {
		checkpoint.deferFn()
	}
	return checkpoint.err
}

type checkpointWithoutDeferredPage struct {
	Checkpoint
}

func TestDeferRejectsExhaustedAndMissingRetryDeadlines(t *testing.T) {
	frontier := NewFrontier(1, nil)
	for _, work := range []crawljob.CrawlJob{
		{RunID: uuid.New(), RetryAttempt: ^uint8(0), URL: "https://example.org/page"},
		{RunID: uuid.New(), URL: "https://example.org/page"},
	} {
		consumed, scheduled := frontier.Defer(t.Context(), work, time.Time{})
		if consumed || scheduled {
			t.Fatalf("invalid retry returned %t/%t", consumed, scheduled)
		}
	}
}

func TestDeferRequeuesStaleLeaseForCurrentLease(t *testing.T) {
	profile := internalProfile(t)
	provenance := []byte("stale-retry-lease")
	frontier := NewFrontier(2, nil)
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:   internalRequests(profile, "https://example.org/page"),
			Provenance: provenance, LeaseID: "old-lease",
		},
		profile,
		nil,
	)
	stale := internalReceive(t, frontier)
	if result := frontier.RebindRunLease(
		provenance,
		"old-lease",
		"current-lease",
	); result != RunLeaseRebound {
		t.Fatalf("rebind result = %d, want rebound", result)
	}
	consumed, scheduled := frontier.Defer(t.Context(), stale, time.Now().Add(time.Minute))
	if !consumed || scheduled {
		t.Fatalf("stale retry result = %t/%t, want consumed and unscheduled", consumed, scheduled)
	}
	replayed := internalReceive(t, frontier)
	if replayed.RunID != seeded.RunID || replayed.LeaseID != "current-lease" {
		t.Fatalf("replayed lease = %q on run %s", replayed.LeaseID, replayed.RunID)
	}
	frontier.Done(replayed, successfulPageOutcome())
}

func TestDeferPersistsRetryAndPassesLiveCheckpointContext(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("durable-retry-identity")
	provenance := []byte("durable-retry-provenance")
	base := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	checkpoint := &deferredPageRecordingCheckpoint{Checkpoint: base}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.org/durable"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		nil,
	)
	job := internalReceive(t, frontier)
	retryAt := time.Now().Add(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	consumed, scheduled := frontier.Defer(ctx, job, retryAt)
	if !consumed || !scheduled || checkpoint.ctxErr != nil ||
		checkpoint.pageURL != job.URL || checkpoint.attempt != 1 ||
		!checkpoint.retryAt.Equal(retryAt) {
		t.Fatalf("durable retry = %t/%t, checkpoint = %+v", consumed, scheduled, checkpoint)
	}
	frontier.mu.Lock()
	run := frontier.state.runs[seeded.RunID]
	if run == nil || len(frontier.state.ready) != 1 ||
		frontier.state.ready[0].RetryAttempt != 1 ||
		!frontier.state.ready[0].RetryAfter.Equal(retryAt) {
		frontier.mu.Unlock()
		t.Fatalf("durable retry state = run=%+v ready=%+v", run, frontier.state.ready)
	}
	frontier.mu.Unlock()
}

func TestDeferRecordsMissingAndFailedCheckpointCapabilities(t *testing.T) {
	writeFailure := errors.New("retry checkpoint failure")
	for _, test := range []struct {
		name      string
		configure func(*deferredPageRecordingCheckpoint)
		wantError error
	}{
		{
			name:      "missing capability",
			configure: func(*deferredPageRecordingCheckpoint) {},
			wantError: frontiercheckpoint.ErrInvalidPage,
		},
		{
			name: "write failure",
			configure: func(checkpoint *deferredPageRecordingCheckpoint) {
				checkpoint.err = writeFailure
			},
			wantError: writeFailure,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := internalProfile(t)
			identity := []byte("failed-retry-identity-" + test.name)
			provenance := []byte("failed-retry-provenance-" + test.name)
			base := &scriptedCheckpoint{
				snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
			}
			checkpoint := &deferredPageRecordingCheckpoint{Checkpoint: base}
			test.configure(checkpoint)
			var installed Checkpoint = checkpoint
			if test.name == "missing capability" {
				installed = checkpointWithoutDeferredPage{Checkpoint: base}
			}
			frontier := NewFrontier(1, nil, WithCheckpoint(installed))
			frontier.SeedRunWithPriority(
				t.Context(),
				CrawlRunSeed{
					Requests:      internalRequests(profile, "https://example.org/durable"),
					Provenance:    provenance,
					OrderIdentity: identity,
				},
				profile,
				nil,
			)
			job := internalReceive(t, frontier)
			consumed, scheduled := frontier.Defer(
				t.Context(),
				job,
				time.Now().Add(time.Minute),
			)
			if !consumed || scheduled || !errors.Is(frontier.CheckpointFailure(), test.wantError) {
				t.Fatalf(
					"checkpoint retry = %t/%t, failure = %v",
					consumed, scheduled, frontier.CheckpointFailure(),
				)
			}
		})
	}
}

func TestDeferFinishesDurabilityWhenRunChangesDuringCheckpoint(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("changed-retry-identity")
	provenance := []byte("changed-retry-provenance")
	base := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	var frontier *Frontier
	var runID uuid.UUID
	checkpoint := &deferredPageRecordingCheckpoint{
		Checkpoint: base,
		deferFn: func() {
			frontier.mu.Lock()
			delete(frontier.state.runs, runID)
			frontier.mu.Unlock()
		},
	}
	frontier = NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.org/durable"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		nil,
	)
	runID = seeded.RunID
	job := internalReceive(t, frontier)
	consumed, scheduled := frontier.Defer(t.Context(), job, time.Now().Add(time.Minute))
	if !consumed || !scheduled {
		t.Fatalf("durability completion after run change = %t/%t", consumed, scheduled)
	}
	if _, found := frontier.state.runs[seeded.RunID]; found {
		t.Fatal("run changed during checkpoint remained installed")
	}
}
