package transfertally

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

func openMemTally(t *testing.T) *Tally {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	tally, err := Open(v)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return tally
}

func TestTallyAccumulatesTransferTotals(t *testing.T) {
	ctx := context.Background()
	tally := openMemTally(t)

	steps := []struct {
		add func(context.Context, int) error
		n   int
	}{
		{tally.AddSentWords, 5},
		{tally.AddSentWords, 7},
		{tally.AddReceivedWords, 3},
		{tally.AddSentURLs, 2},
		{tally.AddReceivedURLs, 9},
		{tally.AddReceivedURLs, 0},
		{tally.AddSentURLs, -4},
	}
	for _, step := range steps {
		if err := step.add(ctx, step.n); err != nil {
			t.Fatalf("add %d: %v", step.n, err)
		}
	}

	totals, err := tally.Totals(ctx)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	want := Totals{SentWords: 12, ReceivedWords: 3, SentURLs: 2, ReceivedURLs: 9}
	if totals != want {
		t.Fatalf("totals = %+v, want %+v", totals, want)
	}
}

func TestTallyRejectsSecondRegistration(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if _, err := Open(v); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatal("second Open on one vault did not fail")
	}
}

type tallyStubEngine struct {
	buckets  map[vault.Name]map[string][]byte
	putError error
}

func newTallyStubEngine() *tallyStubEngine {
	return &tallyStubEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func (e *tallyStubEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(tallyStubTxn{engine: e, writable: true})
}

func (e *tallyStubEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(tallyStubTxn{engine: e})
}

func (e *tallyStubEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *tallyStubEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *tallyStubEngine) QuotaBytes() int64                        { return 0 }
func (e *tallyStubEngine) Close() error                             { return nil }

type tallyStubTxn struct {
	engine   *tallyStubEngine
	writable bool
}

func (t tallyStubTxn) Bucket(name vault.Name) vault.EngineBucket {
	return tallyStubBucket{engine: t.engine, name: name}
}

func (t tallyStubTxn) Writable() bool { return t.writable }

type tallyStubBucket struct {
	engine *tallyStubEngine
	name   vault.Name
}

func (b tallyStubBucket) Get(key vault.Key) []byte {
	raw := b.engine.buckets[b.name][string(key)]
	if raw == nil {
		return nil
	}

	return append([]byte(nil), raw...)
}

func (b tallyStubBucket) Put(key vault.Key, raw []byte) error {
	if b.engine.putError != nil {
		return b.engine.putError
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (b tallyStubBucket) Delete(key vault.Key) error {
	delete(b.engine.buckets[b.name], string(key))

	return nil
}

func (b tallyStubBucket) Scan(vault.Key, func(vault.Key, []byte) (bool, error)) error {
	return nil
}

func openStubTally(t *testing.T, engine *tallyStubEngine) *Tally {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	tally, err := Open(storage)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return tally
}

func TestTallySurvivesReopenOnSameStorage(t *testing.T) {
	ctx := context.Background()
	engine := newTallyStubEngine()
	if err := openStubTally(t, engine).AddSentWords(ctx, 11); err != nil {
		t.Fatalf("AddSentWords: %v", err)
	}

	totals, err := openStubTally(t, engine).Totals(ctx)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if totals.SentWords != 11 {
		t.Fatalf("sent words = %d, want 11 after reopen", totals.SentWords)
	}
}

func TestTallyReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()

	broken := newTallyStubEngine()
	broken.putError = errors.New("disk full")
	if err := openStubTally(t, broken).AddSentURLs(ctx, 1); err == nil {
		t.Error("store failure did not fail")
	}

	corrupt := newTallyStubEngine()
	tally := openStubTally(t, corrupt)
	corrupt.buckets[tallyBucket][string(receivedWordsKey)] = []byte("not-a-number")
	if err := tally.AddReceivedWords(ctx, 1); err == nil {
		t.Error("corrupt total did not fail add")
	}
	if _, err := tally.Totals(ctx); err == nil {
		t.Error("corrupt total did not fail totals")
	}
}
