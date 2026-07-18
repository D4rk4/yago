package transfertally

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
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
	buckets        map[vault.Name]map[string][]byte
	putError       error
	updateError    error
	failUpdateCall int
	updateCalls    int
}

func newTallyStubEngine() *tallyStubEngine {
	return &tallyStubEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func (e *tallyStubEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	e.updateCalls++
	if e.updateCalls == e.failUpdateCall {
		return e.updateError
	}

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
	tally := openStubTally(t, engine)
	if err := tally.AddSentWords(ctx, 11); err != nil {
		t.Fatalf("AddSentWords: %v", err)
	}
	if err := tally.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
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
	tally := openStubTally(t, broken)
	if err := tally.AddSentURLs(ctx, 1); err != nil {
		t.Fatalf("AddSentURLs: %v", err)
	}
	if err := tally.Flush(ctx); err == nil {
		t.Error("store failure did not fail flush")
	}
	broken.putError = nil
	if err := tally.Flush(ctx); err != nil {
		t.Fatalf("retry Flush: %v", err)
	}
	totals, err := tally.Totals(ctx)
	if err != nil || totals.SentURLs != 1 {
		t.Fatalf("restored pending total = %+v, %v", totals, err)
	}

	corrupt := newTallyStubEngine()
	tally = openStubTally(t, corrupt)
	corrupt.buckets[tallyBucket][string(receivedWordsKey)] = []byte("not-a-number")
	if err := tally.AddReceivedWords(ctx, 1); err != nil {
		t.Fatalf("AddReceivedWords: %v", err)
	}
	if err := tally.Flush(ctx); err == nil {
		t.Error("corrupt total did not fail flush")
	}
	if _, err := tally.Totals(ctx); err == nil {
		t.Error("corrupt total did not fail totals")
	}
}

func TestTallyCoalescesUpdatesUntilFlush(t *testing.T) {
	ctx := context.Background()
	engine := newTallyStubEngine()
	tally := openStubTally(t, engine)
	for range 100 {
		if err := tally.AddReceivedWords(ctx, 1); err != nil {
			t.Fatalf("AddReceivedWords: %v", err)
		}
	}
	if engine.updateCalls != 0 {
		t.Fatalf("updates before flush = %d", engine.updateCalls)
	}
	if err := tally.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if engine.updateCalls != 1 {
		t.Fatalf("updates after flush = %d, want one", engine.updateCalls)
	}
	if err := tally.Flush(ctx); err != nil {
		t.Fatalf("empty Flush: %v", err)
	}
	if engine.updateCalls != 1 {
		t.Fatalf("empty flush updates = %d, want one", engine.updateCalls)
	}
}

func TestTallyRetriesOnlyCountersWhoseFlushDidNotCommit(t *testing.T) {
	ctx := context.Background()
	engine := newTallyStubEngine()
	tally := openStubTally(t, engine)
	for _, add := range []struct {
		apply func(context.Context, int) error
		value int
	}{
		{tally.AddSentWords, 2},
		{tally.AddReceivedWords, 3},
		{tally.AddSentURLs, 5},
	} {
		if err := add.apply(ctx, add.value); err != nil {
			t.Fatalf("add transfer total: %v", err)
		}
	}
	engine.failUpdateCall = 2
	engine.updateError = errors.New("second shard unavailable")
	if err := tally.Flush(ctx); err == nil {
		t.Fatal("later counter failure did not fail flush")
	}
	engine.failUpdateCall = 0
	if err := tally.Flush(ctx); err != nil {
		t.Fatalf("retry Flush: %v", err)
	}

	totals, err := openStubTally(t, engine).Totals(ctx)
	if err != nil {
		t.Fatalf("Totals after retry: %v", err)
	}
	want := Totals{SentWords: 2, ReceivedWords: 3, SentURLs: 5}
	if totals != want {
		t.Fatalf("totals after retry = %+v, want %+v", totals, want)
	}
}

func TestTallyRejectsCanceledAdd(t *testing.T) {
	tally := openMemTally(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := tally.AddSentWords(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("AddSentWords error = %v", err)
	}
	totals, err := tally.Totals(context.Background())
	if err != nil || totals.SentWords != 0 {
		t.Fatalf("totals = %+v, %v", totals, err)
	}
}

func TestTallyPreservesConcurrentAddsAcrossFlushAndTotals(t *testing.T) {
	ctx := t.Context()
	tally := openMemTally(t)
	const additions = 5000
	start := make(chan struct{})
	errorsSeen := make(chan error, 5)
	var writers sync.WaitGroup
	for _, add := range []func(context.Context, int) error{
		tally.AddSentWords,
		tally.AddReceivedWords,
		tally.AddSentURLs,
		tally.AddReceivedURLs,
	} {
		writers.Add(1)
		go repeatTransferTallyAdds(transferTallyAddRun{
			ctx: ctx, start: start, errorsSeen: errorsSeen, writers: &writers,
			additions: additions, add: add,
		})
	}
	monitorDone := make(chan struct{})
	go observeConcurrentTransferTally(ctx, tally, start, monitorDone, errorsSeen)
	close(start)
	writers.Wait()
	<-monitorDone
	close(errorsSeen)
	assertNoConcurrentTransferTallyErrors(t, errorsSeen)
	if err := tally.Flush(ctx); err != nil {
		t.Fatalf("final Flush: %v", err)
	}
	totals, err := tally.Totals(ctx)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	assertTransferTallyTotals(t, totals, Totals{
		SentWords:     additions,
		ReceivedWords: additions,
		SentURLs:      additions,
		ReceivedURLs:  additions,
	})
}

type transferTallyAddRun struct {
	ctx        context.Context
	start      <-chan struct{}
	errorsSeen chan<- error
	writers    *sync.WaitGroup
	additions  int
	add        func(context.Context, int) error
}

func repeatTransferTallyAdds(run transferTallyAddRun) {
	defer run.writers.Done()
	<-run.start
	for range run.additions {
		if err := run.add(run.ctx, 1); err != nil {
			run.errorsSeen <- err

			return
		}
		runtime.Gosched()
	}
}

func observeConcurrentTransferTally(
	ctx context.Context,
	tally *Tally,
	start <-chan struct{},
	done chan<- struct{},
	errorsSeen chan<- error,
) {
	defer close(done)
	<-start
	for range 250 {
		if err := tally.Flush(ctx); err != nil {
			errorsSeen <- err

			return
		}
		if _, err := tally.Totals(ctx); err != nil {
			errorsSeen <- err

			return
		}
		runtime.Gosched()
	}
}

func assertNoConcurrentTransferTallyErrors(t *testing.T, errorsSeen <-chan error) {
	t.Helper()
	for err := range errorsSeen {
		t.Fatalf("concurrent tally operation: %v", err)
	}
}

func assertTransferTallyTotals(t *testing.T, got, want Totals) {
	t.Helper()
	if got != want {
		t.Fatalf("totals = %+v, want %+v", got, want)
	}
}
