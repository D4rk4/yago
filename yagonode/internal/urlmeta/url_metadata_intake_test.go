package urlmeta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{Hash: yagomodel.WordHash("self"), NetworkName: "freeworld"}
}

type urlPorts struct {
	Directory URLDirectory
	Evictor   URLEvictor
	Receiver  URLReceiver
	Vault     *vault.Vault
}

func openModule(t *testing.T, quotaBytes int64) urlPorts {
	t.Helper()

	v, err := memvault.Open(quotaBytes)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	directory, evictor, receiver, err := Open(v)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return urlPorts{Directory: directory, Evictor: evictor, Receiver: receiver, Vault: v}
}

func urlRow(t *testing.T, seed string) yagomodel.URIMetadataRow {
	t.Helper()

	row := yagomodel.URIMetadataRow{
		Properties: map[string]string{yagomodel.URLMetaHash: yagomodel.WordHash(seed).String()},
	}
	roundTrip, err := yagomodel.ParseURIMetadataRow(row.String())
	if err != nil {
		t.Fatalf("row does not round-trip: %v", err)
	}

	return roundTrip
}

func rowHash(t *testing.T, row yagomodel.URIMetadataRow) yagomodel.Hash {
	t.Helper()

	hash, err := row.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}

	return hash.Hash()
}

func TestIntakePersistsAndReportsExisting(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 0)
	first := urlRow(t, "a")
	second := urlRow(t, "b")

	receipt, err := module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{first, second})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if receipt.Busy || receipt.Double != 0 || len(receipt.ErrorURL) != 0 {
		t.Fatalf("first receipt = %+v, want empty", receipt)
	}

	receipt, err = module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{first})
	if err != nil {
		t.Fatalf("Intake duplicate: %v", err)
	}
	if receipt.Double != 1 {
		t.Fatalf("duplicate Double = %d, want 1", receipt.Double)
	}

	count, err := module.Directory.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Fatalf("Count = %d, want 2", count)
	}
}

func TestIntakeDurabilityAndLookup(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 0)
	row := urlRow(t, "a")
	hash := rowHash(t, row)

	if _, err := module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{row}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	rows, err := module.Directory.RowsByHash(ctx, []yagomodel.Hash{hash})
	if err != nil {
		t.Fatalf("RowsByHash: %v", err)
	}
	if len(rows) != 1 || rowHash(t, rows[0]) != hash {
		t.Fatalf("RowsByHash = %v, want one matching row", rows)
	}

	missing, err := module.Directory.MissingURLs(ctx, []yagomodel.Hash{
		hash,
		yagomodel.WordHash("absent"),
		yagomodel.WordHash("absent"),
	})
	if err != nil {
		t.Fatalf("MissingURLs: %v", err)
	}
	if len(missing) != 1 || missing[0] != yagomodel.WordHash("absent") {
		t.Fatalf("MissingURLs = %v, want one absent hash", missing)
	}
}

func TestIntakeBusyAtCapacity(t *testing.T) {
	ctx := context.Background()
	module := openModule(t, 1)

	receipt, err := module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{urlRow(t, "a")})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if receipt.Busy {
		t.Fatalf("first receipt = %+v, want stored", receipt)
	}
	if _, err := module.Vault.UsedBytes(ctx); err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}

	receipt, err = module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{urlRow(t, "b")})
	if err != nil {
		t.Fatalf("Intake over capacity: %v", err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %+v, want Busy", receipt)
	}
}

func TestIntakeNotifiesObserverOfStoredURLs(t *testing.T) {
	ctx := context.Background()
	observer := &recordingObserver{}
	_, module := openObservedModule(t, observer)
	row := urlRow(t, "a")

	if _, err := module.Receiver.Receive(ctx, []yagomodel.URIMetadataRow{row}); err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if len(observer.stored) != 1 || observer.stored[0] != rowHash(t, row) {
		t.Fatalf("stored = %v, want one matching hash", observer.stored)
	}
}

func TestIntakeSurvivesObserverFailure(t *testing.T) {
	ctx := context.Background()
	observer := &recordingObserver{fail: true}
	_, module := openObservedModule(t, observer)

	if _, err := module.Receiver.Receive(
		ctx,
		[]yagomodel.URIMetadataRow{urlRow(t, "a")},
	); err != nil {
		t.Fatalf("Intake: %v", err)
	}
	count, err := module.Directory.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("Count = %d, want 1 despite observer failure", count)
	}
}

// contendOnceEngine simulates the shard engine's contention retry: the first
// update pass sees a contended Put, and — exactly like shardvault — the
// engine re-runs the callback only when the callback RETURNS the contended
// error. A callback that swallows it (the STOR-05 bug) commits a pass that
// silently dropped the row.
type contendOnceEngine struct {
	mu      sync.Mutex
	buckets map[vault.Name]map[string][]byte
	passes  int
	retried bool
}

func newContendOnceEngine() *contendOnceEngine {
	return &contendOnceEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func (e *contendOnceEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.passes++
	txn := plainTxn{engine: e}
	var err error
	if e.passes == 1 {
		err = fn(contendedTxn{plainTxn: txn})
	} else {
		err = fn(txn)
	}
	if err != nil && errors.Is(err, vault.ErrContended) {
		e.retried = true
		e.mu.Unlock()
		defer e.mu.Lock()

		return e.Update(ctx, fn)
	}

	return err
}

func (e *contendOnceEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return fn(plainTxn{engine: e})
}

func (e *contendOnceEngine) Provision(name vault.Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *contendOnceEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *contendOnceEngine) QuotaBytes() int64                        { return 0 }
func (e *contendOnceEngine) Close() error                             { return nil }

type plainTxn struct{ engine *contendOnceEngine }

func (t plainTxn) Writable() bool { return true }

func (t plainTxn) Bucket(name vault.Name) vault.EngineBucket {
	if t.engine.buckets[name] == nil {
		t.engine.buckets[name] = map[string][]byte{}
	}

	return plainBucket{data: t.engine.buckets[name]}
}

type plainBucket struct{ data map[string][]byte }

func (b plainBucket) Get(key vault.Key) []byte { return b.data[string(key)] }

func (b plainBucket) Put(key vault.Key, value []byte) error {
	b.data[string(key)] = append([]byte(nil), value...)

	return nil
}

func (b plainBucket) Delete(key vault.Key) error {
	delete(b.data, string(key))

	return nil
}

func (b plainBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	for key, value := range b.data {
		if len(prefix) > 0 && !strings.HasPrefix(key, string(prefix)) {
			continue
		}
		if ok, err := fn(vault.Key(key), value); !ok || err != nil {
			return err
		}
	}

	return nil
}

type contendedTxn struct{ plainTxn }

func (t contendedTxn) Bucket(name vault.Name) vault.EngineBucket {
	return contendedBucket{EngineBucket: t.plainTxn.Bucket(name)}
}

type contendedBucket struct{ vault.EngineBucket }

func (contendedBucket) Put(vault.Key, []byte) error {
	return fmt.Errorf("shard contended: %w", vault.ErrContended)
}

// TestIntakeRetriesContendedStoreInsteadOfDropping pins STOR-05: a contended
// Put must propagate out of the update callback so the engine's exclusive
// retry runs — the row lands on the second pass instead of being discarded.
func TestIntakeRetriesContendedStoreInsteadOfDropping(t *testing.T) {
	engine := newContendOnceEngine()
	store, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	directory, _, receiver, err := Open(store)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	row := urlRow(t, "contended-row")

	receipt, err := receiver.Receive(context.Background(), []yagomodel.URIMetadataRow{row})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(receipt.ErrorURL) != 0 {
		t.Fatalf("row reported rejected despite retryable contention: %+v", receipt)
	}
	if !engine.retried {
		t.Fatal("the contended pass must trigger the engine retry")
	}
	rows, err := directory.RowsByHash(context.Background(), []yagomodel.Hash{rowHash(t, row)})
	if err != nil || len(rows) != 1 {
		t.Fatalf("row missing after retry: rows=%d err=%v", len(rows), err)
	}
}

func TestIntakeLogsDiscardAfterSuccessfulReplay(t *testing.T) {
	engine := newContendOnceEngine()
	store, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, _, receiver, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	_, err = receiver.Receive(t.Context(), []yagomodel.URIMetadataRow{
		{Properties: map[string]string{}},
		urlRow(t, "replayed-log"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(output.String(), urlRowDiscarded); count != 1 {
		t.Fatalf("discard logs = %d, want one: %s", count, output.String())
	}
}

func TestIntakeLogsObserverFailureOnceAfterReplay(t *testing.T) {
	observer := &recordingObserver{fail: true}
	_, module, engine := openScriptedModule(t, observer)
	engine.replayNext = true
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if _, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{urlRow(t, "observer-replay")},
	); err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(output.String(), urlObserverFailed); count != 1 {
		t.Fatalf("observer logs = %d, want one: %s", count, output.String())
	}
}
