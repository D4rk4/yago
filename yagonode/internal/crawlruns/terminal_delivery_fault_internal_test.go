package crawlruns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errTerminalStorageFault = errors.New("terminal storage fault")

type terminalFaultEngine struct {
	buckets        map[vault.Name]map[string][]byte
	putFailures    map[vault.Name]error
	deleteFailures map[vault.Name]error
	scanFailures   map[vault.Name]error
	replayNext     bool
}

func newTerminalFaultEngine() *terminalFaultEngine {
	return &terminalFaultEngine{
		buckets:        make(map[vault.Name]map[string][]byte),
		putFailures:    make(map[vault.Name]error),
		deleteFailures: make(map[vault.Name]error),
		scanFailures:   make(map[vault.Name]error),
	}
}

func (e *terminalFaultEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = make(map[string][]byte)
	}

	return nil
}

func (e *terminalFaultEngine) Update(
	ctx context.Context,
	operation func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("terminal update context: %w", err)
	}
	if e.replayNext {
		e.replayNext = false
		candidate := cloneTerminalBuckets(e.buckets)
		if err := operation(terminalFaultTransaction{
			engine:   e,
			buckets:  candidate,
			writable: true,
		}); err != nil {
			return err
		}
	}
	candidate := cloneTerminalBuckets(e.buckets)
	if err := operation(terminalFaultTransaction{
		engine:   e,
		buckets:  candidate,
		writable: true,
	}); err != nil {
		return err
	}
	e.buckets = candidate

	return nil
}

func (e *terminalFaultEngine) View(
	ctx context.Context,
	operation func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("terminal view context: %w", err)
	}

	return operation(terminalFaultTransaction{engine: e, buckets: e.buckets})
}

func (*terminalFaultEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func (*terminalFaultEngine) QuotaBytes() int64 { return 0 }

func (*terminalFaultEngine) Close() error { return nil }

type terminalFaultTransaction struct {
	engine   *terminalFaultEngine
	buckets  map[vault.Name]map[string][]byte
	writable bool
}

func (t terminalFaultTransaction) Bucket(name vault.Name) vault.EngineBucket {
	entries := t.buckets[name]
	if entries == nil {
		entries = make(map[string][]byte)
		t.buckets[name] = entries
	}

	return terminalFaultBucket{engine: t.engine, name: name, entries: entries}
}

func (t terminalFaultTransaction) Writable() bool { return t.writable }

type terminalFaultBucket struct {
	engine  *terminalFaultEngine
	name    vault.Name
	entries map[string][]byte
}

func (b terminalFaultBucket) Get(key vault.Key) []byte {
	return bytes.Clone(b.entries[string(key)])
}

func (b terminalFaultBucket) Put(key vault.Key, value []byte) error {
	if err := b.engine.putFailures[b.name]; err != nil {
		return err
	}
	b.entries[string(key)] = bytes.Clone(value)

	return nil
}

func (b terminalFaultBucket) Delete(key vault.Key) error {
	if err := b.engine.deleteFailures[b.name]; err != nil {
		return err
	}
	delete(b.entries, string(key))

	return nil
}

func (b terminalFaultBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
	if err := b.engine.scanFailures[b.name]; err != nil {
		return err
	}
	keys := make([]string, 0, len(b.entries))
	for key := range b.entries {
		if strings.HasPrefix(key, string(prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		keepGoing, err := visit(vault.Key(key), bytes.Clone(b.entries[key]))
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
	}

	return nil
}

func cloneTerminalBuckets(
	source map[vault.Name]map[string][]byte,
) map[vault.Name]map[string][]byte {
	clone := make(map[vault.Name]map[string][]byte, len(source))
	for name, entries := range source {
		clonedEntries := make(map[string][]byte, len(entries))
		for key, value := range entries {
			clonedEntries[key] = bytes.Clone(value)
		}
		clone[name] = clonedEntries
	}

	return clone
}

func openTerminalFaultRegistry(
	t *testing.T,
	engine *terminalFaultEngine,
	capacity int,
) (*Registry, *vault.Vault) {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open fault vault: %v", err)
	}
	registry, err := Open(t.Context(), storage, capacity)
	if err != nil {
		t.Fatalf("open terminal registry: %v", err)
	}

	return registry, storage
}

func durableTerminalRecord(
	runID string,
	identity byte,
	updated time.Time,
	confirmed bool,
) terminalDeliveryRecord {
	progress := terminalProgress(runID, uint64(identity))

	return terminalDeliveryRecord{
		Identity:  terminalIdentity(identity),
		Progress:  progress,
		Run:       terminalRunFromProgress(progress, Run{}, false, updated),
		Confirmed: confirmed,
	}
}

func persistTerminalRecord(
	t *testing.T,
	registry *Registry,
	key string,
	record terminalDeliveryRecord,
) {
	t.Helper()
	if err := registry.terminal.storage.Update(t.Context(), func(transaction *vault.Txn) error {
		return registry.terminal.deliveries.Put(transaction, vault.Key(key), record)
	}); err != nil {
		t.Fatalf("persist terminal fixture: %v", err)
	}
}

func reopenTerminalFaultRegistry(
	t *testing.T,
	engine *terminalFaultEngine,
	capacity int,
) (*Registry, error) {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		return nil, fmt.Errorf("reopen fault vault: %w", err)
	}

	return Open(t.Context(), storage, capacity)
}

func corruptTerminalLength(engine *terminalFaultEngine) {
	for name, entries := range engine.buckets {
		if name != terminalDeliveryBucket {
			entries[string(terminalDeliveryBucket)] = []byte{1}
		}
	}
}

func TestVolatileTerminalDeliveryRetainsEffectiveRate(t *testing.T) {
	registry := New(2)
	progress := terminalProgress("run", 3)
	progress.RateKnown = true
	progress.PagesPerMinute = 47
	identity := terminalIdentity(1)
	if err := registry.RecordTerminal(t.Context(), identity, progress); err != nil {
		t.Fatalf("record volatile terminal progress: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err != nil {
		t.Fatalf("confirm volatile terminal progress: %v", err)
	}
	run := registry.Recent()[0]
	if !run.RateKnown || run.PagesPerMinute != 47 {
		t.Fatalf("effective rate = %d/%v, want 47/true", run.PagesPerMinute, run.RateKnown)
	}
}

func TestTerminalDeliveryRejectsInvalidProgressBeforeMutation(t *testing.T) {
	tests := []struct {
		name     string
		identity []byte
		progress yagocrawlcontract.CrawlRunProgress
	}{
		{name: "identity", identity: []byte("short"), progress: terminalProgress("run", 1)},
		{name: "run", identity: terminalIdentity(1), progress: terminalProgress("", 1)},
		{
			name:     "state",
			identity: terminalIdentity(1),
			progress: yagocrawlcontract.CrawlRunProgress{
				RunID: "run", State: yagocrawlcontract.CrawlRunRunning,
			},
		},
		{
			name:     "pending",
			identity: terminalIdentity(1),
			progress: yagocrawlcontract.CrawlRunProgress{
				RunID: "run", State: yagocrawlcontract.CrawlRunFinished,
				Tally: yagocrawlcontract.CrawlRunTally{Pending: 1},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := New(1)
			if err := registry.RecordTerminal(
				t.Context(),
				test.identity,
				test.progress,
			); err == nil {
				t.Fatal("invalid terminal progress accepted")
			}
			if registry.Len() != 0 {
				t.Fatalf("invalid terminal progress created %d runs", registry.Len())
			}
		})
	}
}

func TestConfirmationEvictsConfirmedOverflow(t *testing.T) {
	engine := newTerminalFaultEngine()
	registry, _ := openTerminalFaultRegistry(t, engine, 1)
	firstIdentity := terminalIdentity(1)
	if err := registry.RecordTerminal(
		t.Context(),
		firstIdentity,
		terminalProgress("one", 1),
	); err != nil {
		t.Fatalf("record first terminal progress: %v", err)
	}
	if err := registry.RecordTerminal(
		t.Context(),
		terminalIdentity(2),
		terminalProgress("two", 2),
	); err != nil {
		t.Fatalf("record second terminal progress: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), firstIdentity); err != nil {
		t.Fatalf("confirm first terminal progress: %v", err)
	}
	if _, retained := registry.terminal.records["one"]; retained {
		t.Fatal("confirmed overflow remained in the durable ledger")
	}
	if _, retained := engine.buckets[terminalDeliveryBucket]["one"]; retained {
		t.Fatal("confirmed overflow remained in storage")
	}
}

func TestTerminalDeliveryEvictionsResetAcrossTransactionReplay(t *testing.T) {
	t.Run("record", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, _ := openTerminalFaultRegistry(t, engine, 1)
		first := durableTerminalRecord("one", 1, time.Unix(1, 0), true)
		persistTerminalRecord(t, registry, first.Run.RunID, first)
		engine.replayNext = true
		evicted, err := registry.recordTerminalDeliveryLocked(
			t.Context(),
			durableTerminalRecord("two", 2, time.Unix(2, 0), false),
		)
		if err != nil {
			t.Fatalf("record replayed terminal delivery: %v", err)
		}
		if len(evicted) != 1 || evicted[0].Run.RunID != first.Run.RunID {
			t.Fatalf("record replay evictions = %+v", evicted)
		}
	})

	t.Run("confirm", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, _ := openTerminalFaultRegistry(t, engine, 1)
		first := durableTerminalRecord("one", 1, time.Unix(1, 0), false)
		second := durableTerminalRecord("two", 2, time.Unix(2, 0), false)
		persistTerminalRecord(t, registry, first.Run.RunID, first)
		persistTerminalRecord(t, registry, second.Run.RunID, second)
		engine.replayNext = true
		evicted, err := registry.confirmTerminalDeliveryLocked(t.Context(), first)
		if err != nil {
			t.Fatalf("confirm replayed terminal delivery: %v", err)
		}
		if len(evicted) != 1 || evicted[0].Run.RunID != first.Run.RunID {
			t.Fatalf("confirm replay evictions = %+v", evicted)
		}
	})
}

func TestTerminalDeliveryLoadPrunesOldestConfirmedRecords(t *testing.T) {
	engine := newTerminalFaultEngine()
	registry, _ := openTerminalFaultRegistry(t, engine, 8)
	base := time.Unix(4000, 0)
	first := durableTerminalRecord("a", 1, base, true)
	second := durableTerminalRecord("b", 2, base, true)
	third := durableTerminalRecord("c", 3, base.Add(time.Second), true)
	for _, record := range []terminalDeliveryRecord{first, second, third} {
		persistTerminalRecord(t, registry, record.Run.RunID, record)
	}
	if !terminalDeliveryLess(first, second) {
		t.Fatal("equal-time deliveries were not ordered by run id")
	}

	reloaded, err := reopenTerminalFaultRegistry(t, engine, 1)
	if err != nil {
		t.Fatalf("reload bounded terminal registry: %v", err)
	}
	runs := reloaded.Recent()
	if len(runs) != 1 || runs[0].RunID != "c" {
		t.Fatalf("reloaded runs = %+v, want newest run c", runs)
	}
	if len(engine.buckets[terminalDeliveryBucket]) != 1 {
		t.Fatalf(
			"durable terminal deliveries = %d, want 1",
			len(engine.buckets[terminalDeliveryBucket]),
		)
	}
}

func TestTerminalDeliveryLoadRejectsMismatchedKey(t *testing.T) {
	engine := newTerminalFaultEngine()
	registry, _ := openTerminalFaultRegistry(t, engine, 4)
	persistTerminalRecord(
		t,
		registry,
		"other",
		durableTerminalRecord("run", 1, time.Unix(1, 0), false),
	)
	if _, err := reopenTerminalFaultRegistry(t, engine, 4); err == nil {
		t.Fatal("terminal delivery with a mismatched key loaded")
	}
}

func TestTerminalDeliveryLoadRejectsDuplicateIdentity(t *testing.T) {
	engine := newTerminalFaultEngine()
	registry, _ := openTerminalFaultRegistry(t, engine, 4)
	first := durableTerminalRecord("one", 1, time.Unix(1, 0), false)
	second := durableTerminalRecord("two", 2, time.Unix(2, 0), false)
	second.Identity = bytes.Clone(first.Identity)
	for _, record := range []terminalDeliveryRecord{first, second} {
		persistTerminalRecord(t, registry, record.Run.RunID, record)
	}
	if _, err := reopenTerminalFaultRegistry(t, engine, 4); !errors.Is(
		err,
		errTerminalDeliveryConflict,
	) {
		t.Fatalf("duplicate terminal identity error = %v", err)
	}
}

func TestTerminalDeliveryLoadSurfacesPruneDeleteFailure(t *testing.T) {
	engine := newTerminalFaultEngine()
	registry, _ := openTerminalFaultRegistry(t, engine, 4)
	for index, runID := range []string{"one", "two"} {
		record := durableTerminalRecord(
			runID,
			byte(index+1),
			time.Unix(int64(index+1), 0),
			true,
		)
		persistTerminalRecord(t, registry, runID, record)
	}
	engine.deleteFailures[terminalDeliveryBucket] = errTerminalStorageFault
	if _, err := reopenTerminalFaultRegistry(t, engine, 1); err == nil {
		t.Fatal("terminal prune delete failure was ignored")
	}
	if len(engine.buckets[terminalDeliveryBucket]) != 2 {
		t.Fatalf(
			"failed prune retained %d records, want 2",
			len(engine.buckets[terminalDeliveryBucket]),
		)
	}
}

func TestTerminalDeliveryRecordReconcilesDurableState(t *testing.T) {
	t.Run("idempotent", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, _ := openTerminalFaultRegistry(t, engine, 2)
		record := durableTerminalRecord("run", 1, time.Unix(1, 0), false)
		persistTerminalRecord(t, registry, record.Run.RunID, record)
		if err := registry.RecordTerminal(
			t.Context(),
			record.Identity,
			record.Progress,
		); err != nil {
			t.Fatalf("reconcile matching durable state: %v", err)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, _ := openTerminalFaultRegistry(t, engine, 2)
		record := durableTerminalRecord("run", 1, time.Unix(1, 0), false)
		persistTerminalRecord(t, registry, record.Run.RunID, record)
		if err := registry.RecordTerminal(
			t.Context(),
			terminalIdentity(2),
			record.Progress,
		); !errors.Is(err, errTerminalDeliveryConflict) {
			t.Fatalf("conflicting durable state error = %v", err)
		}
	})
}

func TestTerminalDeliveryRecordSurfacesCorruptionAndWriteFailure(t *testing.T) {
	t.Run("corruption", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, _ := openTerminalFaultRegistry(t, engine, 2)
		engine.buckets[terminalDeliveryBucket]["run"] = []byte{terminalDeliveryRecordFormat, '{'}
		if err := registry.RecordTerminal(
			t.Context(),
			terminalIdentity(1),
			terminalProgress("run", 1),
		); err == nil {
			t.Fatal("corrupt durable terminal record was accepted")
		}
	})

	t.Run("write", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, _ := openTerminalFaultRegistry(t, engine, 2)
		engine.putFailures[terminalDeliveryBucket] = errTerminalStorageFault
		if err := registry.RecordTerminal(
			t.Context(),
			terminalIdentity(1),
			terminalProgress("run", 1),
		); err == nil {
			t.Fatal("terminal storage write failure was ignored")
		}
		if registry.Len() != 0 || len(engine.buckets[terminalDeliveryBucket]) != 0 {
			t.Fatal("failed terminal write mutated memory or storage")
		}
	})
}

func TestTerminalDeliveryConfirmationSurfacesDurableStateFailure(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*terminalFaultEngine, terminalDeliveryRecord)
	}{
		{
			name: "missing",
			mutate: func(engine *terminalFaultEngine, record terminalDeliveryRecord) {
				delete(engine.buckets[terminalDeliveryBucket], record.Run.RunID)
			},
		},
		{
			name: "corrupt",
			mutate: func(engine *terminalFaultEngine, record terminalDeliveryRecord) {
				engine.buckets[terminalDeliveryBucket][record.Run.RunID] = []byte{
					terminalDeliveryRecordFormat,
					'{',
				}
			},
		},
		{
			name: "conflict",
			mutate: func(engine *terminalFaultEngine, record terminalDeliveryRecord) {
				conflict := durableTerminalRecord(record.Run.RunID, 9, time.Unix(9, 0), false)
				encoded, _ := terminalDeliveryCodec{}.Encode(conflict)
				engine.buckets[terminalDeliveryBucket][record.Run.RunID] = encoded
			},
		},
		{
			name: "write",
			mutate: func(engine *terminalFaultEngine, _ terminalDeliveryRecord) {
				engine.putFailures[terminalDeliveryBucket] = errTerminalStorageFault
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := newTerminalFaultEngine()
			registry, _ := openTerminalFaultRegistry(t, engine, 2)
			record := durableTerminalRecord("run", 1, time.Unix(1, 0), false)
			if err := registry.RecordTerminal(
				t.Context(),
				record.Identity,
				record.Progress,
			); err != nil {
				t.Fatalf("record terminal fixture: %v", err)
			}
			test.mutate(engine, record)
			if err := registry.ConfirmTerminalDelivery(
				t.Context(),
				record.Identity,
			); err == nil {
				t.Fatal("durable confirmation failure was ignored")
			}
			if registry.terminal.records[record.Run.RunID].Confirmed {
				t.Fatal("failed durable confirmation mutated memory")
			}
		})
	}
}

func TestTerminalDeliveryConfirmationRetriesAfterWriteFailure(t *testing.T) {
	engine := newTerminalFaultEngine()
	registry, _ := openTerminalFaultRegistry(t, engine, 2)
	identity := terminalIdentity(1)
	if err := registry.RecordTerminal(
		t.Context(),
		identity,
		terminalProgress("run", 1),
	); err != nil {
		t.Fatalf("record terminal fixture: %v", err)
	}
	engine.putFailures[terminalDeliveryBucket] = errTerminalStorageFault
	if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err == nil {
		t.Fatal("terminal confirmation write failure was ignored")
	}
	delete(engine.putFailures, terminalDeliveryBucket)
	if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err != nil {
		t.Fatalf("retry terminal confirmation: %v", err)
	}
	if !registry.terminal.records["run"].Confirmed {
		t.Fatal("retried terminal confirmation was not retained")
	}
}

func TestTerminalDeliveryPruningSurfacesLengthAndScanFailures(t *testing.T) {
	t.Run("length", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, storage := openTerminalFaultRegistry(t, engine, 1)
		corruptTerminalLength(engine)
		var removed []terminalDeliveryRecord
		err := storage.Update(t.Context(), func(transaction *vault.Txn) error {
			return registry.pruneConfirmedTerminalDeliveries(transaction, &removed)
		})
		if err == nil {
			t.Fatal("corrupt terminal delivery length was accepted")
		}
	})

	t.Run("scan", func(t *testing.T) {
		engine := newTerminalFaultEngine()
		registry, _ := openTerminalFaultRegistry(t, engine, 1)
		identity := terminalIdentity(1)
		if err := registry.RecordTerminal(
			t.Context(),
			identity,
			terminalProgress("one", 1),
		); err != nil {
			t.Fatalf("record terminal fixture: %v", err)
		}
		if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err != nil {
			t.Fatalf("confirm terminal fixture: %v", err)
		}
		engine.scanFailures[terminalDeliveryBucket] = errTerminalStorageFault
		if err := registry.RecordTerminal(
			t.Context(),
			terminalIdentity(2),
			terminalProgress("two", 2),
		); err == nil {
			t.Fatal("terminal prune scan failure was ignored")
		}
		if len(engine.buckets[terminalDeliveryBucket]) != 1 {
			t.Fatal("failed terminal prune committed a partial transaction")
		}
	})
}

func TestTerminalDeliveryPruningSurfacesDeleteFailure(t *testing.T) {
	engine := newTerminalFaultEngine()
	registry, _ := openTerminalFaultRegistry(t, engine, 1)
	identity := terminalIdentity(1)
	if err := registry.RecordTerminal(
		t.Context(),
		identity,
		terminalProgress("one", 1),
	); err != nil {
		t.Fatalf("record terminal fixture: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), identity); err != nil {
		t.Fatalf("confirm terminal fixture: %v", err)
	}
	engine.deleteFailures[terminalDeliveryBucket] = errTerminalStorageFault
	if err := registry.RecordTerminal(
		t.Context(),
		terminalIdentity(2),
		terminalProgress("two", 2),
	); err == nil {
		t.Fatal("terminal prune delete failure was ignored")
	}
	if len(engine.buckets[terminalDeliveryBucket]) != 1 {
		t.Fatal("failed terminal prune committed a partial transaction")
	}
}
