package frontier

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestPersistentCancellationSettlesExactlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open cancellation checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	profile := internalProfile(t)
	provenance := []byte("exact-cancellation-settlement")
	identity := bytes.Repeat([]byte{7}, sha256.Size)
	settlement := crawlsettlement.Settlement{
		LeaseID:         "lease-cancellation",
		OrderIdentity:   identity,
		Provenance:      provenance,
		WorkerID:        "worker",
		WorkerSessionID: "session",
		Outcome:         crawlsettlement.Delete,
		State:           yagocrawlcontract.CrawlRunCancelled,
	}
	var callbacks atomic.Int64
	callbackErrors := make(chan error, 2)
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.com/cancelled"),
			Provenance:    provenance,
			OrderIdentity: identity,
			LeaseID:       settlement.LeaseID,
		},
		profile,
		func(succeeded bool) {
			callbacks.Add(1)
			if !succeeded {
				callbackErrors <- errUnexpectedCancellationFailure

				return
			}
			callbackErrors <- checkpoint.Stage(t.Context(), settlement)
		},
	)
	if !frontier.CancelControl(provenance) {
		t.Fatal("persistent cancellation was not durable")
	}
	frontier.WaitForSettlements()
	if err := <-callbackErrors; err != nil {
		t.Fatalf("stage cancellation settlement: %v", err)
	}
	if frontier.CancelControl(provenance) {
		t.Fatal("replayed cancellation unexpectedly targeted an active run")
	}
	frontier.WaitForSettlements()
	if callbacks.Load() != 1 {
		t.Fatalf("cancellation settlement callbacks = %d, want 1", callbacks.Load())
	}
	if err := checkpoint.Stage(t.Context(), settlement); err != nil {
		t.Fatalf("repeat cancellation settlement stage: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close cancellation settlement checkpoint: %v", err)
	}
	checkpoint, err = frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen cancellation settlement checkpoint: %v", err)
	}
	awaiting, err := checkpoint.Awaiting(t.Context())
	if err != nil || len(awaiting) != 1 ||
		!crawlsettlement.SameDefinition(awaiting[0], settlement) {
		t.Fatalf("durable cancellation settlement = %+v, %v", awaiting, err)
	}
}

var errUnexpectedCancellationFailure = errors.New("cancelled run settled as failed")
