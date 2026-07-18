package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
)

const migrationTestOrderBucket Name = "orders"

type migrationTestEngine struct {
	mu              sync.RWMutex
	buckets         map[Name]map[string][]byte
	closed          bool
	viewErr         error
	viewSequence    []error
	updateErr       error
	provisionErrors map[Name]error
	putErrors       map[Name]error
	scanErrors      map[Name]error
	afterPageRead   func()
	beforeScan      func()
}

func newMigrationTestVault(t *testing.T) (*Vault, *migrationTestEngine) {
	t.Helper()
	engine := newMigrationTestEngine()
	storage, err := New(engine)
	if err != nil {
		t.Fatalf("open migration test vault: %v", err)
	}

	return storage, engine
}

func newMigrationTestEngine() *migrationTestEngine {
	return &migrationTestEngine{
		buckets:         map[Name]map[string][]byte{},
		provisionErrors: map[Name]error{},
		putErrors:       map[Name]error{},
		scanErrors:      map[Name]error{},
	}
}

func (e *migrationTestEngine) AtomicUpdates() bool { return true }

func (e *migrationTestEngine) BucketProvisioned(ctx context.Context, name Name) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("migration test bucket context: %w", err)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return false, errVaultClosed
	}
	_, present := e.buckets[name]

	return present, nil
}

func (e *migrationTestEngine) Provision(name Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return errVaultClosed
	}
	if err := e.provisionErrors[name]; err != nil {
		return err
	}
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *migrationTestEngine) Update(ctx context.Context, fn func(EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("migration test update context: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return errVaultClosed
	}
	if e.updateErr != nil {
		return e.updateErr
	}
	staged := cloneMigrationBuckets(e.buckets)
	if err := fn(migrationTestTxn{engine: e, buckets: staged, writable: true}); err != nil {
		return err
	}
	e.buckets = staged

	return nil
}

func (e *migrationTestEngine) View(ctx context.Context, fn func(EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("migration test view context: %w", err)
	}
	if len(e.viewSequence) > 0 {
		err := e.viewSequence[0]
		e.viewSequence = e.viewSequence[1:]
		if err != nil {
			return err
		}
	}
	if e.viewErr != nil {
		return e.viewErr
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return errVaultClosed
	}

	return fn(migrationTestTxn{engine: e, buckets: e.buckets})
}

func (e *migrationTestEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *migrationTestEngine) QuotaBytes() int64                        { return 0 }

func (e *migrationTestEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true

	return nil
}

type nonAtomicMigrationEngine struct {
	inner *migrationTestEngine
}

type atomicMigrationEngineWithoutPresence struct {
	nonAtomicMigrationEngine
}

func (atomicMigrationEngineWithoutPresence) AtomicUpdates() bool { return true }

func (e nonAtomicMigrationEngine) Provision(name Name) error {
	return e.inner.Provision(name)
}

func (e nonAtomicMigrationEngine) Update(ctx context.Context, fn func(EngineTxn) error) error {
	return e.inner.Update(ctx, fn)
}

func (e nonAtomicMigrationEngine) View(ctx context.Context, fn func(EngineTxn) error) error {
	return e.inner.View(ctx, fn)
}

func (e nonAtomicMigrationEngine) UsedBytes(ctx context.Context) (int64, error) {
	return e.inner.UsedBytes(ctx)
}

func (e nonAtomicMigrationEngine) QuotaBytes() int64 { return e.inner.QuotaBytes() }
func (e nonAtomicMigrationEngine) Close() error      { return e.inner.Close() }

type migrationTestTxn struct {
	engine   *migrationTestEngine
	buckets  map[Name]map[string][]byte
	writable bool
}

func (t migrationTestTxn) Bucket(name Name) EngineBucket {
	return migrationTestBucket{engine: t.engine, name: name, entries: t.buckets[name]}
}

func (t migrationTestTxn) Writable() bool { return t.writable }

type migrationTestBucket struct {
	engine  *migrationTestEngine
	name    Name
	entries map[string][]byte
}

func (b migrationTestBucket) Get(key Key) []byte {
	return append([]byte(nil), b.entries[string(key)]...)
}

func (b migrationTestBucket) Put(key Key, value []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	b.entries[string(key)] = append([]byte(nil), value...)

	return nil
}

func (b migrationTestBucket) Delete(key Key) error {
	delete(b.entries, string(key))

	return nil
}

func (b migrationTestBucket) Scan(prefix Key, fn func(Key, []byte) (bool, error)) error {
	if b.engine.beforeScan != nil {
		b.engine.beforeScan()
	}
	if err := b.engine.scanErrors[b.name]; err != nil {
		return err
	}
	for _, key := range orderedMigrationKeys(b.entries, prefix) {
		keep, err := fn(Key(key), b.entries[key])
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func (b migrationTestBucket) ReadPageAfter(after Key, limit int) (BucketPage, error) {
	ordered := orderedMigrationKeys(b.entries, nil)
	start := sort.Search(len(ordered), func(index int) bool {
		return ordered[index] > string(after)
	})
	end := min(start+limit, len(ordered))
	entries := make([]BucketPageEntry, 0, end-start)
	for _, key := range ordered[start:end] {
		entries = append(entries, BucketPageEntry{Key: Key(key), Value: b.entries[key]})
	}
	if b.engine.afterPageRead != nil {
		b.engine.afterPageRead()
	}

	return BucketPage{Entries: entries, More: end < len(ordered)}, nil
}

func orderedMigrationKeys(entries map[string][]byte, prefix Key) []string {
	ordered := make([]string, 0, len(entries))
	for key := range entries {
		if strings.HasPrefix(key, string(prefix)) {
			ordered = append(ordered, key)
		}
	}
	sort.Strings(ordered)

	return ordered
}

func cloneMigrationBuckets(source map[Name]map[string][]byte) map[Name]map[string][]byte {
	cloned := make(map[Name]map[string][]byte, len(source))
	for name, entries := range source {
		cloned[name] = make(map[string][]byte, len(entries))
		for key, value := range entries {
			cloned[name][key] = append([]byte(nil), value...)
		}
	}

	return cloned
}

func putMigrationOrderRows(t *testing.T, storage *Vault, rows int) {
	t.Helper()
	if err := provisionRetainedBuckets(storage, []Name{migrationTestOrderBucket}); err != nil {
		t.Fatalf("provision source: %v", err)
	}
	if err := storage.Update(context.Background(), func(tx *Txn) error {
		for row := range rows {
			key := Key(fmt.Sprintf("%04d", row))
			if err := tx.etx.Bucket(migrationTestOrderBucket).Put(
				key,
				[]byte("value-"+string(key)),
			); err != nil {
				return fmt.Errorf("put migration test order %s: %w", key, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("populate source: %v", err)
	}
}

func assertMigrationOrderRows(t *testing.T, storage *Vault, rows int) {
	t.Helper()
	collection, err := Register(storage, migrationTestOrderBucket, internalStringCodec{})
	if err != nil {
		t.Fatalf("register migrated bucket: %v", err)
	}
	if err := storage.View(context.Background(), func(tx *Txn) error {
		length, err := collection.Len(tx)
		if err != nil {
			return err
		}
		if length != rows {
			return fmt.Errorf("length = %d, want %d", length, rows)
		}
		seen := 0
		if err := collection.Scan(tx, nil, func(_ Key, _ string) (bool, error) {
			seen++

			return true, nil
		}); err != nil {
			return err
		}
		if seen != rows {
			return fmt.Errorf("rows = %d, want %d", seen, rows)
		}

		return nil
	}); err != nil {
		t.Fatalf("verify migrated rows: %v", err)
	}
}

func TestRetainedBucketMigrationLeavesAbsentSourceBucketUnprovisioned(t *testing.T) {
	source, sourceEngine := newMigrationTestVault(t)
	target, _ := newMigrationTestVault(t)
	if _, present := sourceEngine.buckets[migrationTestOrderBucket]; present {
		t.Fatal("source bucket was provisioned before migration")
	}
	if err := MigrateRetainedBuckets(
		context.Background(),
		source,
		target,
		"migration",
		"1",
		[]Name{migrationTestOrderBucket},
	); err != nil {
		t.Fatalf("migrate absent source bucket: %v", err)
	}
	if _, present := sourceEngine.buckets[migrationTestOrderBucket]; present {
		t.Fatal("migration provisioned absent source bucket")
	}
	assertMigrationOrderRows(t, target, 0)
}

func TestRetainedBucketMigrationDefaultBoundaries(t *testing.T) {
	if err := afterRetainedBucketMigrationPage("orders", nil); err != nil {
		t.Fatalf("default page boundary: %v", err)
	}
	if err := beforeRetainedBucketCompletion(); err != nil {
		t.Fatalf("default completion boundary: %v", err)
	}
	if err := MigrateRetainedBuckets(
		context.Background(), nil, nil, "", "", nil,
	); err == nil {
		t.Fatal("invalid migration accepted")
	}
}

func TestRetainedBucketMigrationResumesAfterPageInterruption(t *testing.T) {
	source, sourceEngine := newMigrationTestVault(t)
	target, _ := newMigrationTestVault(t)
	putMigrationOrderRows(t, source, retainedBucketMigrationPageSize+44)
	sourceBefore := cloneMigrationBuckets(sourceEngine.buckets)
	sentinel := errors.New("power lost")
	pages := 0
	afterRetainedBucketMigrationPage = func(Name, Key) error {
		pages++
		if pages == 1 {
			return sentinel
		}

		return nil
	}
	t.Cleanup(func() { afterRetainedBucketMigrationPage = func(Name, Key) error { return nil } })

	err := MigrateRetainedBuckets(
		context.Background(),
		source,
		target,
		"migration",
		"1",
		[]Name{"orders"},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("interrupted migration error = %v, want %v", err, sentinel)
	}
	afterRetainedBucketMigrationPage = func(Name, Key) error { return nil }
	if err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	); err != nil {
		t.Fatalf("resume migration: %v", err)
	}
	assertMigrationOrderRows(t, target, retainedBucketMigrationPageSize+44)
	if !migrationBucketsEqual(sourceBefore, sourceEngine.buckets) {
		t.Fatal("source changed during retained migration")
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}
	if err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	); err != nil {
		t.Fatalf("completed migration touched source: %v", err)
	}
}

func TestRetainedBucketMigrationResumesBeforeCompletion(t *testing.T) {
	source, _ := newMigrationTestVault(t)
	target, _ := newMigrationTestVault(t)
	putMigrationOrderRows(t, source, 3)
	sentinel := errors.New("power lost")
	beforeRetainedBucketCompletion = func() error { return sentinel }
	t.Cleanup(func() { beforeRetainedBucketCompletion = func() error { return nil } })

	err := MigrateRetainedBuckets(
		context.Background(),
		source,
		target,
		"migration",
		"1",
		[]Name{"orders"},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("interrupted completion error = %v, want %v", err, sentinel)
	}
	beforeRetainedBucketCompletion = func() error { return nil }
	if err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	); err != nil {
		t.Fatalf("resume completion: %v", err)
	}
	assertMigrationOrderRows(t, target, 3)
}

func TestRetainedBucketMigrationFailsClosedOnMarkerAndPrecompletionMismatch(t *testing.T) {
	t.Run("marker", func(t *testing.T) {
		source, _ := newMigrationTestVault(t)
		target, _ := newMigrationTestVault(t)
		putMigrationOrderRows(t, source, 1)
		if err := MigrateRetainedBuckets(
			context.Background(), source, target, "migration", "1", []Name{"orders"},
		); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		err := MigrateRetainedBuckets(
			context.Background(), source, target, "migration", "2", []Name{"orders"},
		)
		if err == nil || !strings.Contains(err.Error(), "marker mismatch") {
			t.Fatalf("marker mismatch error = %v", err)
		}
	})

	t.Run("precompletion fingerprint", func(t *testing.T) {
		source, _ := newMigrationTestVault(t)
		target, _ := newMigrationTestVault(t)
		putMigrationOrderRows(t, source, 1)
		if err := provisionRetainedBuckets(target, []Name{"orders"}); err != nil {
			t.Fatal(err)
		}
		if err := target.Update(context.Background(), func(tx *Txn) error {
			return tx.etx.Bucket("orders").Put(Key("extra"), []byte("extra"))
		}); err != nil {
			t.Fatalf("seed mismatched target: %v", err)
		}
		err := MigrateRetainedBuckets(
			context.Background(), source, target, "migration", "1", []Name{"orders"},
		)
		if err == nil || !strings.Contains(err.Error(), "verification mismatch") {
			t.Fatalf("fingerprint mismatch error = %v", err)
		}
	})
}

func TestCompletedRetainedBucketMigrationKeepsLiveTargetMutations(t *testing.T) {
	source, _ := newMigrationTestVault(t)
	target, _ := newMigrationTestVault(t)
	putMigrationOrderRows(t, source, 1)
	if err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	orders, err := Register(target, "orders", internalStringCodec{})
	if err != nil {
		t.Fatalf("register target orders: %v", err)
	}
	if err := target.Update(context.Background(), func(tx *Txn) error {
		return orders.Put(tx, Key("live"), "live-value")
	}); err != nil {
		t.Fatalf("mutate target: %v", err)
	}
	if err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	); err != nil {
		t.Fatalf("rerun completed migration: %v", err)
	}
	if err := target.View(context.Background(), func(tx *Txn) error {
		value, found, err := orders.Get(tx, Key("live"))
		if err != nil {
			return err
		}
		if !found || value != "live-value" {
			return fmt.Errorf("live target value = %q found=%t", value, found)
		}
		length, err := orders.Len(tx)
		if err != nil {
			return err
		}
		if length != 2 {
			return fmt.Errorf("live target length = %d, want 2", length)
		}

		return nil
	}); err != nil {
		t.Fatalf("verify live target: %v", err)
	}
}

func TestRetainedBucketMigrationAcceptsIdenticalTargetEntry(t *testing.T) {
	engine := newMigrationTestEngine()
	entries := map[string][]byte{"0000": []byte("value")}
	bucket := migrationTestBucket{engine: engine, name: "orders", entries: entries}
	if err := putRetainedBucketMigrationEntry(
		bucket,
		"orders",
		BucketPageEntry{Key: Key("0000"), Value: []byte("value")},
	); err != nil {
		t.Fatalf("identical target entry: %v", err)
	}
}

func TestRetainedBucketMigrationRejectsTargetConflict(t *testing.T) {
	source, _ := newMigrationTestVault(t)
	target, _ := newMigrationTestVault(t)
	putMigrationOrderRows(t, source, 1)
	if err := provisionRetainedBuckets(target, []Name{"orders"}); err != nil {
		t.Fatal(err)
	}
	if err := target.Update(context.Background(), func(tx *Txn) error {
		return tx.etx.Bucket("orders").Put(Key("0000"), []byte("different"))
	}); err != nil {
		t.Fatal(err)
	}
	err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	)
	if err == nil || !strings.Contains(err.Error(), "target conflict") {
		t.Fatalf("target conflict error = %v", err)
	}
}

func TestRetainedBucketMigrationRejectsNonAtomicTarget(t *testing.T) {
	source, _ := newMigrationTestVault(t)
	engine := newMigrationTestEngine()
	target, err := New(nonAtomicMigrationEngine{inner: engine})
	if err != nil {
		t.Fatal(err)
	}
	err = MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	)
	if err == nil || !strings.Contains(err.Error(), "target is not atomic") {
		t.Fatalf("non-atomic target error = %v", err)
	}
}

func TestRetainedBucketMigrationRejectsUnknownTargetBucketPresence(t *testing.T) {
	source, _ := newMigrationTestVault(t)
	engine := newMigrationTestEngine()
	target, err := New(atomicMigrationEngineWithoutPresence{
		nonAtomicMigrationEngine{inner: engine},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	)
	if err == nil || !strings.Contains(err.Error(), "does not report bucket presence") {
		t.Fatalf("unknown target presence error = %v", err)
	}
}

func TestRetainedBucketMigrationRejectsUnknownSourceBucketPresence(t *testing.T) {
	engine := newMigrationTestEngine()
	source, err := New(atomicMigrationEngineWithoutPresence{
		nonAtomicMigrationEngine{inner: engine},
	})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := newMigrationTestVault(t)
	err = MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	)
	if err == nil || !strings.Contains(err.Error(), "does not report bucket presence") {
		t.Fatalf("unknown source presence error = %v", err)
	}
}

func TestRetainedBucketMigrationFailsPageCursorWrite(t *testing.T) {
	source, _ := newMigrationTestVault(t)
	target, targetEngine := newMigrationTestVault(t)
	putMigrationOrderRows(t, source, 1)
	sentinel := errors.New("page cursor write failed")
	targetEngine.putErrors["migration"] = sentinel
	if err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	); !errors.Is(err, sentinel) {
		t.Fatalf("page cursor write error = %v, want %v", err, sentinel)
	}
}

type migrationStorageBoundaryCase struct {
	name      string
	rows      int
	configure func(*migrationTestEngine, *migrationTestEngine, error)
}

func retainedMigrationStorageBoundaryCases() []migrationStorageBoundaryCase {
	return []migrationStorageBoundaryCase{
		{
			name: "marker provision",
			configure: func(_ *migrationTestEngine, target *migrationTestEngine, failure error) {
				target.provisionErrors["migration"] = failure
			},
		},
		{
			name: "target provision",
			configure: func(_ *migrationTestEngine, target *migrationTestEngine, failure error) {
				target.provisionErrors["orders"] = failure
			},
		},
		{
			name: "source page read",
			configure: func(source *migrationTestEngine, _ *migrationTestEngine, failure error) {
				source.viewErr = failure
			},
		},
		{
			name: "target page write",
			rows: 1,
			configure: func(_ *migrationTestEngine, target *migrationTestEngine, failure error) {
				target.putErrors["orders"] = failure
			},
		},
		{
			name: "source fingerprint",
			rows: 1,
			configure: func(source *migrationTestEngine, _ *migrationTestEngine, failure error) {
				source.scanErrors["orders"] = failure
			},
		},
		{
			name: "target fingerprint",
			rows: 1,
			configure: func(_ *migrationTestEngine, target *migrationTestEngine, failure error) {
				target.scanErrors["orders"] = failure
			},
		},
		{
			name: "length commit",
			rows: 1,
			configure: func(_ *migrationTestEngine, target *migrationTestEngine, failure error) {
				target.putErrors[lengthBucket] = failure
			},
		},
		{
			name: "marker commit",
			configure: func(_ *migrationTestEngine, target *migrationTestEngine, failure error) {
				target.putErrors["migration"] = failure
			},
		},
		{
			name: "completion transaction",
			rows: 1,
			configure: func(_ *migrationTestEngine, target *migrationTestEngine, failure error) {
				beforeRetainedBucketCompletion = func() error {
					target.updateErr = failure

					return nil
				}
			},
		},
	}
}

func TestRetainedBucketMigrationFailsClosedAtStorageBoundaries(t *testing.T) {
	sentinel := errors.New("storage failed")
	for _, test := range retainedMigrationStorageBoundaryCases() {
		t.Run(test.name, func(t *testing.T) {
			assertRetainedMigrationStorageBoundary(t, test, sentinel)
		})
	}
}

func assertRetainedMigrationStorageBoundary(
	t *testing.T,
	test migrationStorageBoundaryCase,
	sentinel error,
) {
	t.Helper()
	source, sourceEngine := newMigrationTestVault(t)
	target, targetEngine := newMigrationTestVault(t)
	putMigrationOrderRows(t, source, test.rows)
	test.configure(sourceEngine, targetEngine, sentinel)
	t.Cleanup(func() {
		beforeRetainedBucketCompletion = func() error { return nil }
	})
	err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("migration error = %v, want %v", err, sentinel)
	}
}

func TestRetainedBucketMigrationRejectsCursorRaces(t *testing.T) {
	source, sourceEngine := newMigrationTestVault(t)
	target, _ := newMigrationTestVault(t)
	putMigrationOrderRows(t, source, 1)
	sourceEngine.afterPageRead = func() {
		if err := target.Update(context.Background(), func(tx *Txn) error {
			return tx.etx.Bucket("migration").Put(
				retainedMigrationCursorKey("orders"),
				[]byte("different"),
			)
		}); err != nil {
			t.Fatalf("move migration cursor: %v", err)
		}
	}
	err := MigrateRetainedBuckets(
		context.Background(), source, target, "migration", "1", []Name{"orders"},
	)
	if err == nil || !strings.Contains(err.Error(), "cursor mismatch") {
		t.Fatalf("cursor race error = %v", err)
	}
}

func TestRetainedBucketMigrationContextAndClosedStorage(t *testing.T) {
	t.Run("closed atomic target", func(t *testing.T) {
		source, _ := newMigrationTestVault(t)
		target, _ := newMigrationTestVault(t)
		if err := target.Close(); err != nil {
			t.Fatal(err)
		}
		if err := MigrateRetainedBuckets(
			context.Background(), source, target, "migration", "1", []Name{"orders"},
		); !errors.Is(err, errVaultClosed) {
			t.Fatalf("closed target error = %v", err)
		}
	})

	t.Run("closed source", func(t *testing.T) {
		source, _ := newMigrationTestVault(t)
		target, _ := newMigrationTestVault(t)
		if err := source.Close(); err != nil {
			t.Fatal(err)
		}
		if err := MigrateRetainedBuckets(
			context.Background(), source, target, "migration", "1", []Name{"orders"},
		); !errors.Is(err, errVaultClosed) {
			t.Fatalf("closed source error = %v", err)
		}
	})

	t.Run("cursor read", func(t *testing.T) {
		target, engine := newMigrationTestVault(t)
		engine.viewErr = errors.New("cursor read failed")
		if _, err := retainedBucketCursor(
			context.Background(), target, "migration", "orders",
		); err == nil || !strings.Contains(err.Error(), "cursor read failed") {
			t.Fatalf("cursor error = %v", err)
		}
	})

	t.Run("migration cursor read", func(t *testing.T) {
		source, _ := newMigrationTestVault(t)
		target, engine := newMigrationTestVault(t)
		putMigrationOrderRows(t, source, 1)
		sentinel := errors.New("migration cursor read failed")
		engine.viewSequence = []error{nil, sentinel}
		if err := MigrateRetainedBuckets(
			context.Background(), source, target, "migration", "1", []Name{"orders"},
		); !errors.Is(err, sentinel) {
			t.Fatalf("migration cursor error = %v", err)
		}
	})

	t.Run("fingerprint cancellation", func(t *testing.T) {
		storage, engine := newMigrationTestVault(t)
		putMigrationOrderRows(t, storage, 1)
		ctx, cancel := context.WithCancel(context.Background())
		engine.beforeScan = cancel
		if _, err := retainedBucketFingerprint(
			ctx,
			storage,
			"orders",
		); !errors.Is(
			err,
			context.Canceled,
		) {
			t.Fatalf("fingerprint cancellation error = %v", err)
		}
	})
}

func TestRetainedBucketFingerprintReportsPresenceCancellation(t *testing.T) {
	storage, _ := newMigrationTestVault(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := retainedBucketFingerprint(ctx, storage, "orders")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fingerprint presence cancellation error = %v", err)
	}
}

func TestProvisionRetainedBucketsRejectsClosedStorage(t *testing.T) {
	storage, _ := newMigrationTestVault(t)
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	err := provisionRetainedBuckets(storage, []Name{"orders"})
	if !errors.Is(err, errVaultClosed) {
		t.Fatalf("closed provisioning error = %v", err)
	}
}

func migrationBucketsEqual(left, right map[Name]map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for name, leftEntries := range left {
		rightEntries, found := right[name]
		if !found || len(leftEntries) != len(rightEntries) {
			return false
		}
		for key, value := range leftEntries {
			if !bytes.Equal(value, rightEntries[key]) {
				return false
			}
		}
	}

	return true
}
