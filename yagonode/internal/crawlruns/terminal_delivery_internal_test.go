package crawlruns

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func terminalProgress(runID string, fetched uint64) yagocrawlcontract.CrawlRunProgress {
	return yagocrawlcontract.CrawlRunProgress{
		RunID:         runID,
		WorkerID:      "worker",
		ProfileHandle: "profile",
		ProfileName:   "crawl",
		State:         yagocrawlcontract.CrawlRunFinished,
		Tally:         yagocrawlcontract.CrawlRunTally{Fetched: fetched, Indexed: fetched},
	}
}

func terminalIdentity(value byte) []byte {
	identity := make([]byte, sha256.Size)
	identity[0] = value

	return identity
}

func TestTerminalDeliveryCommitPrecedesRegistryMutation(t *testing.T) {
	storage, err := boltvault.Open(filepath.Join(t.TempDir(), "closed.db"), 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	registry, err := Open(t.Context(), storage, 4)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	observations := collectObservations(registry)
	if err := registry.RecordTerminal(
		t.Context(),
		terminalIdentity(1),
		terminalProgress("run", 1),
	); err == nil {
		t.Fatal("closed storage accepted terminal delivery")
	}
	if registry.Len() != 0 || len(*observations) != 0 {
		t.Fatalf(
			"failed delivery mutated registry: runs=%d observations=%d",
			registry.Len(),
			len(*observations),
		)
	}
}

func TestTerminalDeliveryReloadDeduplicatesCommittedProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	registry, err := Open(t.Context(), storage, 4)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	base := time.Unix(1000, 0)
	registry.now = func() time.Time { return base }
	progress := terminalProgress("run", 3)
	identity := terminalIdentity(2)
	first := collectObservations(registry)
	if err := registry.RecordTerminal(t.Context(), identity, progress); err != nil {
		t.Fatalf("record terminal progress: %v", err)
	}
	if len(*first) != 1 || !(*first)[0].newlyTerminal {
		t.Fatalf("initial observations = %+v", *first)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry, err = Open(t.Context(), storage, 4)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	replayed := collectObservations(registry)
	if err := registry.RecordTerminal(t.Context(), identity, progress); err != nil {
		t.Fatalf("replay terminal progress: %v", err)
	}
	if len(*replayed) != 0 {
		t.Fatalf("replay observations = %+v", *replayed)
	}
	runs := registry.Recent()
	if len(runs) != 1 || runs[0].RunID != "run" || runs[0].Tally.Fetched != 3 ||
		!runs[0].FirstSeen.Equal(base) {
		t.Fatalf("reloaded run = %+v", runs)
	}
	conflict := progress
	conflict.Tally.Fetched++
	if err := registry.RecordTerminal(
		t.Context(),
		identity,
		conflict,
	); !errors.Is(
		err,
		errTerminalDeliveryConflict,
	) {
		t.Fatalf("conflicting replay error = %v", err)
	}
}

func TestTerminalDeliveryPersistenceStaysWithinRegistryCapacity(t *testing.T) {
	storage, err := boltvault.Open(filepath.Join(t.TempDir(), "bounded.db"), 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry, err := Open(t.Context(), storage, 2)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	base := time.Unix(2000, 0)
	for index, runID := range []string{"one", "two", "three"} {
		registry.now = func() time.Time { return base.Add(time.Duration(index) * time.Second) }
		if err := registry.RecordTerminal(
			t.Context(),
			terminalIdentity(byte(index+1)),
			terminalProgress(runID, uint64(index+1)),
		); err != nil {
			t.Fatalf("record %s: %v", runID, err)
		}
		if err := registry.ConfirmTerminalDelivery(
			t.Context(),
			terminalIdentity(byte(index+1)),
		); err != nil {
			t.Fatalf("confirm %s: %v", runID, err)
		}
	}
	if err := storage.View(context.Background(), func(transaction *vault.Txn) error {
		length, err := registry.terminal.deliveries.Len(transaction)
		if err != nil {
			return fmt.Errorf("count durable terminal deliveries: %w", err)
		}
		if length != 2 {
			t.Fatalf("durable deliveries = %d, want 2", length)
		}

		return nil
	}); err != nil {
		t.Fatalf("read durable deliveries: %v", err)
	}
	if registry.Len() != 2 || registry.Recent()[1].RunID != "two" {
		t.Fatalf("bounded recent runs = %+v", registry.Recent())
	}
}

func TestTerminalDeliveryCapacityNeverPrunesUnconfirmedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unconfirmed.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	registry, err := Open(t.Context(), storage, 1)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	first := terminalProgress("one", 1)
	second := terminalProgress("two", 2)
	if err := registry.RecordTerminal(t.Context(), terminalIdentity(1), first); err != nil {
		t.Fatalf("record first: %v", err)
	}
	if err := registry.RecordTerminal(t.Context(), terminalIdentity(2), second); err != nil {
		t.Fatalf("record second: %v", err)
	}
	if err := registry.RecordTerminal(
		t.Context(),
		terminalIdentity(3),
		first,
	); !errors.Is(err, errTerminalDeliveryConflict) {
		t.Fatalf("evicted run conflicting identity error = %v", err)
	}
	if err := registry.RecordTerminal(t.Context(), terminalIdentity(1), first); err != nil {
		t.Fatalf("replay first beyond capacity: %v", err)
	}
	if err := storage.View(t.Context(), func(transaction *vault.Txn) error {
		length, err := registry.terminal.deliveries.Len(transaction)
		if err != nil {
			return fmt.Errorf("count unconfirmed terminal deliveries: %w", err)
		}
		if length != 2 {
			t.Fatalf("unconfirmed durable deliveries = %d, want 2", length)
		}

		return nil
	}); err != nil {
		t.Fatalf("read unconfirmed deliveries: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry, err = Open(t.Context(), storage, 1)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if err := registry.RecordTerminal(t.Context(), terminalIdentity(1), first); err != nil {
		t.Fatalf("replay first after reload: %v", err)
	}
	if runs := registry.Recent(); len(runs) != 1 || runs[0].RunID != "two" {
		t.Fatalf("capacity-one reloaded runs = %+v", runs)
	}
}

func TestTerminalDeliveryConfirmationResumesAfterStorageReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "confirmation.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	registry, err := Open(t.Context(), storage, 1)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	identity := terminalIdentity(4)
	progress := terminalProgress("run", 4)
	if err := registry.RecordTerminal(t.Context(), identity, progress); err != nil {
		t.Fatalf("record terminal: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err == nil {
		t.Fatal("closed storage confirmed terminal delivery")
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry, err = Open(t.Context(), storage, 1)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err != nil {
		t.Fatalf("confirm after reopen: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err != nil {
		t.Fatalf("repeat confirmation: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), terminalIdentity(9)); err != nil {
		t.Fatalf("confirm missing delivery: %v", err)
	}
}

func TestTerminalDeliveryCodecRejectsCorruptState(t *testing.T) {
	codec := terminalDeliveryCodec{}
	for _, raw := range [][]byte{{}, {2, '{', '}'}, {1, '{'}} {
		if _, err := codec.Decode(raw); err == nil {
			t.Fatalf("corrupt terminal delivery %x decoded", raw)
		}
	}
	invalid := terminalDeliveryRecord{
		Identity: terminalIdentity(1),
		Progress: terminalProgress("run", 1),
		Run: Run{
			RunID:     "other",
			FirstSeen: time.Unix(1, 0),
			Updated:   time.Unix(1, 0),
		},
	}
	raw, err := codec.Encode(invalid)
	if err != nil {
		t.Fatalf("encode invalid fixture: %v", err)
	}
	if _, err := codec.Decode(raw); err == nil {
		t.Fatal("invalid terminal delivery record decoded")
	}
	invalid = durableTerminalRecord("run", 1, time.Unix(1, 0), false)
	invalid.Identity = []byte("short")
	raw, err = codec.Encode(invalid)
	if err != nil {
		t.Fatalf("encode invalid progress fixture: %v", err)
	}
	if _, err := codec.Decode(raw); err == nil {
		t.Fatal("invalid terminal progress record decoded")
	}
	if _, err := Open(t.Context(), nil, 1); err == nil {
		t.Fatal("nil storage opened terminal registry")
	}
	if err := New(1).ConfirmTerminalDelivery(t.Context(), []byte("short")); err == nil {
		t.Fatal("short terminal identity confirmed")
	}
}
