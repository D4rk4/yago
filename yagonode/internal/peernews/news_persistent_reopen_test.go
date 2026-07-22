package peernews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type newsReopenProbe struct {
	*newsStubEngine
	root *os.Root
}

const newsReopenStateFile = "peernews-state.json"

func openNewsReopenProbe(directory string) (*newsReopenProbe, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open news reopen probe directory: %w", err)
	}
	probe := &newsReopenProbe{newsStubEngine: newNewsStubEngine(), root: root}
	raw, err := root.ReadFile(newsReopenStateFile)
	if errors.Is(err, os.ErrNotExist) {
		return probe, nil
	}
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("read news reopen probe: %w", err),
			root.Close(),
		)
	}
	if err := json.Unmarshal(raw, &probe.buckets); err != nil {
		return nil, errors.Join(
			fmt.Errorf("decode news reopen probe: %w", err),
			root.Close(),
		)
	}

	return probe, nil
}

func (p *newsReopenProbe) Provision(name vault.Name) error {
	if err := p.newsStubEngine.Provision(name); err != nil {
		return fmt.Errorf("provision news reopen probe: %w", err)
	}

	return p.persist()
}

func (p *newsReopenProbe) Update(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	updateErr := p.newsStubEngine.Update(ctx, fn)
	persistErr := p.persist()
	if updateErr != nil && persistErr != nil {
		return fmt.Errorf(
			"persist news reopen probe after failed update: %w",
			errors.Join(updateErr, persistErr),
		)
	}
	if updateErr != nil {
		return fmt.Errorf("update news reopen probe: %w", updateErr)
	}
	if persistErr != nil {
		return persistErr
	}

	return nil
}

func (p *newsReopenProbe) Close() error {
	persistErr := p.persist()
	closeErr := p.root.Close()
	if persistErr != nil || closeErr != nil {
		return fmt.Errorf(
			"close news reopen probe: %w",
			errors.Join(persistErr, closeErr),
		)
	}

	return nil
}

func (p *newsReopenProbe) persist() error {
	raw, err := json.Marshal(p.buckets)
	if err != nil {
		return fmt.Errorf("encode news reopen probe: %w", err)
	}
	temporary := newsReopenStateFile + ".pending"
	if err := p.root.WriteFile(temporary, raw, 0o600); err != nil {
		return fmt.Errorf("write news reopen probe: %w", err)
	}
	if err := p.root.Rename(temporary, newsReopenStateFile); err != nil {
		return fmt.Errorf("commit news reopen probe: %w", err)
	}

	return nil
}

func openNewsReopenPool(
	t *testing.T,
	directory string,
	now func() time.Time,
) (*Pool, *newsReopenProbe) {
	t.Helper()
	probe, err := openNewsReopenProbe(directory)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := vault.New(probe)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := Open(storage, now)
	if err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}

	return pool, probe
}

func TestNewsRotationRecoversThroughProductionOpenAfterPersistentPartialCommit(t *testing.T) {
	directory := t.TempDir()
	pool, probe := openNewsReopenPool(t, directory, fixedNow)
	record := retentionRecord(fixedNow().Add(-time.Hour), 17, CategoryCrawlStart)
	record.Distributed = distributionLimit - 1
	stored, err := pool.storeNewsRecord(
		t.Context(),
		record,
		fixedNow(),
		[]Queue{Outgoing},
	)
	if err != nil || !stored {
		t.Fatalf("seed outgoing news = %t/%v", stored, err)
	}
	failure := errors.New("later shard commit failed")
	probe.partialCommitTrigger = queueBucket
	probe.partialCommitBuckets = map[vault.Name]bool{queueBucket: true}
	probe.partialCommitFailure = failure
	if _, _, err := pool.NextPublication(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("partial rotation error = %v", err)
	}
	assertPersistedPartialNewsRotation(t, pool, record)
	if err := pool.vault.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, recoveredProbe := openNewsReopenPool(t, directory, fixedNow)
	assertRecoveredNewsRotation(t, recovered, record)
	firstRecoveryState := cloneNewsBuckets(recoveredProbe.buckets)
	if err := recovered.vault.Close(); err != nil {
		t.Fatal(err)
	}

	idempotent, idempotentProbe := openNewsReopenPool(t, directory, fixedNow)
	assertRecoveredNewsRotation(t, idempotent, record)
	if !reflect.DeepEqual(idempotentProbe.buckets, firstRecoveryState) {
		t.Fatal("second production open changed recovered rotation state")
	}
	resumed, found, err := idempotent.NextPublication(t.Context())
	if err != nil || !found || resumed.Distributed != distributionLimit {
		t.Fatalf("first resumed publication = %#v/%t/%v", resumed, found, err)
	}
	if _, found, err := idempotent.ByID(
		t.Context(), Published, record.ID(),
	); err != nil || !found {
		t.Fatalf("first resumed destination = %t/%v", found, err)
	}
	if _, found, err := idempotent.ByID(
		t.Context(), Outgoing, record.ID(),
	); err != nil || found {
		t.Fatalf("first resumed source = %t/%v", found, err)
	}
	assertNoNewsRotationIntent(t, idempotent)
	if err := idempotent.vault.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewsQueueCursorFloorRecoversThroughProductionOpen(t *testing.T) {
	directory := t.TempDir()
	pool, _ := openNewsReopenPool(t, directory, fixedNow)
	first := retentionRecord(fixedNow().Add(-time.Hour), 31, CategoryCrawlStart)
	second := retentionRecord(fixedNow().Add(-time.Hour), 32, CategoryCrawlStart)
	firstKey := queueKey(Incoming, 7)
	secondKey := queueKey(Incoming, 11)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for _, record := range []Record{first, second} {
			if err := putKnownNewsFixture(pool, tx, record); err != nil {
				return err
			}
		}
		if err := pool.queue.Put(tx, firstKey, first.WireForm()); err != nil {
			return fmt.Errorf("seed first queued news: %w", err)
		}
		if err := pool.queue.Put(tx, secondKey, second.WireForm()); err != nil {
			return fmt.Errorf("seed second queued news: %w", err)
		}
		if err := pool.cursor.Put(tx, vault.Key(Incoming), 3); err != nil {
			return fmt.Errorf("seed stale news cursor: %w", err)
		}

		return pool.cleanup.Put(tx, queuedCleanupCursorKey, string(firstKey))
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.vault.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, recoveredProbe := openNewsReopenPool(t, directory, fixedNow)
	assertNewsQueueSequence(t, recovered, Incoming, 11)
	wantPageCursors := []string{"", "", string(firstKey)}
	if got := recoveredProbe.keyPageAfterByBucket[queueBucket]; !slices.Equal(
		got,
		wantPageCursors,
	) {
		t.Fatalf("first reopened queue page cursors = %q, want %q", got, wantPageCursors)
	}
	firstRecoveryState := cloneNewsBuckets(recoveredProbe.buckets)
	if err := recovered.vault.Close(); err != nil {
		t.Fatal(err)
	}

	idempotent, idempotentProbe := openNewsReopenPool(t, directory, fixedNow)
	assertNewsQueueSequence(t, idempotent, Incoming, 11)
	if !reflect.DeepEqual(idempotentProbe.buckets, firstRecoveryState) {
		t.Fatal("second production open changed repaired cursor state")
	}
	third := retentionRecord(fixedNow().Add(-time.Hour), 33, CategoryCrawlStart)
	stored, err := idempotent.EnqueueIncomingNews(t.Context(), third)
	if err != nil || !stored {
		t.Fatalf("first enqueue after reopen = %t/%v", stored, err)
	}
	assertNewsQueueSequence(t, idempotent, Incoming, 12)
	assertQueuedNewsWire(t, idempotent, firstKey, first)
	assertQueuedNewsWire(t, idempotent, secondKey, second)
	assertQueuedNewsWire(t, idempotent, queueKey(Incoming, 12), third)
	if err := idempotent.vault.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertPersistedPartialNewsRotation(t *testing.T, pool *Pool, record Record) {
	t.Helper()
	if _, found, err := pool.ByID(
		t.Context(), Outgoing, record.ID(),
	); err != nil || found {
		t.Fatalf("partial rotation source = %t/%v", found, err)
	}
	rotated, found, err := pool.ByID(t.Context(), Published, record.ID())
	if err != nil || !found || rotated.Distributed != distributionLimit {
		t.Fatalf("partial rotation destination = %#v/%t/%v", rotated, found, err)
	}
	rotation, found, err := pool.readNewsRotation(t.Context())
	if err != nil || !found || rotation.destination != Published ||
		rotation.rotated.Distributed != distributionLimit {
		t.Fatalf("persisted rotation = %#v/%t/%v", rotation, found, err)
	}
}

func assertRecoveredNewsRotation(t *testing.T, pool *Pool, record Record) {
	t.Helper()
	recovered, found, err := pool.ByID(t.Context(), Outgoing, record.ID())
	if err != nil || !found || recovered.WireForm() != record.WireForm() {
		t.Fatalf("recovered rotation source = %#v/%t/%v", recovered, found, err)
	}
	if _, found, err := pool.ByID(
		t.Context(), Published, record.ID(),
	); err != nil || found {
		t.Fatalf("recovered rotation destination = %t/%v", found, err)
	}
	if pool.retentionNeedsReconciliation {
		t.Fatal("recovered rotation remains dirty")
	}
	assertNoNewsRotationIntent(t, pool)
}

func assertNoNewsRotationIntent(t *testing.T, pool *Pool) {
	t.Helper()
	if _, found, err := pool.readNewsRotation(t.Context()); err != nil || found {
		t.Fatalf("news rotation intent = %t/%v", found, err)
	}
}

func assertNewsQueueSequence(t *testing.T, pool *Pool, queue Queue, want uint64) {
	t.Helper()
	var got uint64
	if err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		got, err = pool.storedQueueSequence(tx, queue)

		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s queue sequence = %d, want %d", queue, got, want)
	}
}

func assertQueuedNewsWire(t *testing.T, pool *Pool, key vault.Key, want Record) {
	t.Helper()
	var wire string
	var found bool
	if err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		wire, found, err = pool.queue.Get(tx, key)
		if err != nil {
			return fmt.Errorf("read queued news: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found || wire != want.WireForm() {
		t.Fatalf("queued news %q = %q/%t", key, wire, found)
	}
}
