package frontier

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type redirectMutationCheckpoint struct {
	*scriptedCheckpoint
	recorded bool
	onRecord func()
}

func (checkpoint *redirectMutationCheckpoint) RecordRedirect(
	context.Context,
	[]byte,
	frontiercheckpoint.Redirect,
) (bool, error) {
	if checkpoint.onRecord != nil {
		checkpoint.onRecord()
	}

	return checkpoint.recorded, checkpoint.redirectError
}

func persistentRedirectRun(
	t *testing.T,
	checkpoint Checkpoint,
	identity []byte,
	provenance []byte,
	leaseID string,
) (*Frontier, crawljob.CrawlJob, *crawlRun) {
	t.Helper()
	profile := internalProfile(t)
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.org/source"),
			Provenance:    provenance,
			OrderIdentity: identity,
			LeaseID:       leaseID,
		},
		profile,
		nil,
	)
	work := internalReceive(t, frontier)
	frontier.mu.Lock()
	run := frontier.state.runs[seeded.RunID]
	frontier.mu.Unlock()

	return frontier, work, run
}

func TestPersistentRedirectPropagatesCheckpointFailure(t *testing.T) {
	identity := []byte("redirect-write-failure-identity")
	writeFailure := errors.New("record redirect")
	checkpoint := &scriptedCheckpoint{
		snapshot:      checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		redirectError: writeFailure,
	}
	frontier, work, _ := persistentRedirectRun(
		t,
		checkpoint,
		identity,
		[]byte("redirect-write-failure"),
		"redirect-lease",
	)
	if frontier.ResolveRedirect(work, "https://other.example/target") {
		t.Fatal("failed persistent redirect was admitted")
	}
	if !errors.Is(frontier.CheckpointFailure(), writeFailure) {
		t.Fatalf("checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestPersistentRedirectFencesRunRemovalDuringMutation(t *testing.T) {
	identity := []byte("redirect-run-removal-identity")
	checkpoint := &redirectMutationCheckpoint{
		scriptedCheckpoint: &scriptedCheckpoint{
			snapshot: checkpointSnapshot(
				identity,
				yagocrawlcontract.CrawlOrderPriorityNormal,
			),
		},
		recorded: true,
	}
	frontier, work, _ := persistentRedirectRun(
		t,
		checkpoint,
		identity,
		[]byte("redirect-run-removal"),
		"redirect-lease",
	)
	checkpoint.onRecord = func() {
		frontier.mu.Lock()
		delete(frontier.state.runs, work.RunID)
		frontier.mu.Unlock()
	}
	if !frontier.ResolveRedirect(work, "https://other.example/target") {
		t.Fatal("completed run rejected an already persisted redirect")
	}
}

func TestPersistentRedirectRejectsCorruptPreviousReservation(t *testing.T) {
	identity := []byte("redirect-corruption-identity")
	checkpoint := &scriptedCheckpoint{
		snapshot:          checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		redirectDuplicate: true,
	}
	frontier, work, run := persistentRedirectRun(
		t,
		checkpoint,
		identity,
		[]byte("redirect-corruption"),
		"redirect-lease",
	)
	frontier.mu.Lock()
	run.redirects[work.URL] = redirectReservation{
		URL:      "https://previous.example/target",
		Host:     "previous.example",
		HostBump: true,
	}
	run.visited["https://previous.example/target"] = struct{}{}
	frontier.mu.Unlock()
	if frontier.ResolveRedirect(work, "https://replacement.example/target") {
		t.Fatal("corrupt previous redirect reservation was replaced")
	}
	if !errors.Is(frontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestPersistentRedirectFencesStaleLease(t *testing.T) {
	identity := []byte("redirect-stale-lease-identity")
	checkpoint := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	frontier, work, run := persistentRedirectRun(
		t,
		checkpoint,
		identity,
		[]byte("redirect-stale-lease"),
		"old-lease",
	)
	frontier.mu.Lock()
	run.leaseID = "new-lease"
	frontier.mu.Unlock()
	if frontier.ResolveRedirect(work, "https://other.example/target") {
		t.Fatal("stale lease mutated a persistent redirect")
	}
	if frontier.CheckpointFailure() != nil {
		t.Fatalf("stale lease checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestMemoryRedirectReplacesTargetAndReturnsToSource(t *testing.T) {
	frontier, runID := redirectRunFrontier()
	work := crawljob.CrawlJob{URL: "https://example.com/source", RunID: runID}
	if !frontier.ResolveRedirect(work, "https://first.example/target") ||
		!frontier.ResolveRedirect(work, "https://second.example/target") ||
		!frontier.ResolveRedirect(work, work.URL) {
		t.Fatal("memory redirect replacement was rejected")
	}
	run := frontier.state.runs[runID]
	if len(run.redirects) != 0 {
		t.Fatalf("redirects after returning to source = %v", run.redirects)
	}
}

func TestMemoryRedirectAcceptsDirectFirstResolution(t *testing.T) {
	frontier, runID := redirectRunFrontier()
	work := crawljob.CrawlJob{URL: "https://example.com/source", RunID: runID}
	if !frontier.ResolveRedirect(work, work.URL) {
		t.Fatal("direct first resolution was rejected")
	}
}

func TestMemoryRedirectReleaseValidatesHostOwnership(t *testing.T) {
	run := &crawlRun{
		visited:   map[string]struct{}{"https://same.example/target": {}},
		redirects: map[string]redirectReservation{"source": {}},
		hostPages: make(map[string]int),
	}
	if err := releaseRedirectInMemory(
		run,
		"source",
		redirectReservation{URL: "https://same.example/target"},
	); err != nil {
		t.Fatalf("release same-host redirect: %v", err)
	}
	err := releaseRedirectInMemory(
		run,
		"missing-source",
		redirectReservation{
			URL:      "https://missing.example/target",
			Host:     "missing.example",
			HostBump: true,
		},
	)
	if !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("missing redirect host ownership error = %v", err)
	}
}
