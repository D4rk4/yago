package yagonode

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
)

type recordingTransferTallyFlusher struct {
	mu              sync.Mutex
	contexts        []context.Context
	contextFailures []error
	err             error
	flushed         chan struct{}
}

func (f *recordingTransferTallyFlusher) Flush(ctx context.Context) error {
	f.mu.Lock()
	f.contexts = append(f.contexts, ctx)
	f.contextFailures = append(f.contextFailures, ctx.Err())
	f.mu.Unlock()
	if f.flushed != nil {
		f.flushed <- struct{}{}
	}

	return f.err
}

func TestTransferTallyFlushesPeriodicallyUntilShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 1)
	flusher := &recordingTransferTallyFlusher{flushed: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		runTransferTallyFlush(ctx, flusher, ticks)
		close(done)
	}()

	ticks <- time.Now()
	<-flusher.flushed
	cancel()
	<-done

	flusher.mu.Lock()
	defer flusher.mu.Unlock()
	if len(flusher.contexts) != 1 {
		t.Fatalf("flushes = %d, want one periodic flush", len(flusher.contexts))
	}
}

func TestTransferTallyFlushFailureDoesNotStopLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 1)
	flusher := &recordingTransferTallyFlusher{
		err:     errors.New("flush failed"),
		flushed: make(chan struct{}, 2),
	}
	done := make(chan struct{})
	go func() {
		runTransferTallyFlush(ctx, flusher, ticks)
		close(done)
	}()

	ticks <- time.Now()
	<-flusher.flushed
	ticks <- time.Now()
	<-flusher.flushed
	cancel()
	<-done
}

func TestStartTransferTallyFlushIgnoresAbsentTally(t *testing.T) {
	var background sync.WaitGroup
	startTransferTallyFlush(t.Context(), &background, nil)
	awaitBackgroundAndDrainTransferTally(&background, nil)
}

func TestTransferTallyDrainWaitsForLateProducer(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "transfer-tally.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open tally storage: %v", err)
	}
	tally, err := transfertally.Open(storage)
	if err != nil {
		t.Fatalf("open tally: %v", err)
	}

	producerStarted := make(chan struct{})
	releaseProducer := make(chan struct{})
	producerErrors := make(chan error, 1)
	var background sync.WaitGroup
	background.Add(1)
	go func() {
		defer background.Done()
		close(producerStarted)
		<-releaseProducer
		producerErrors <- tally.AddReceivedWords(ctx, 7)
	}()
	<-producerStarted

	drained := make(chan struct{})
	go func() {
		awaitBackgroundAndDrainTransferTally(&background, tally)
		close(drained)
	}()
	select {
	case <-drained:
		t.Fatal("transfer tally drained before producer stopped")
	default:
	}
	close(releaseProducer)
	<-drained
	if err := <-producerErrors; err != nil {
		t.Fatalf("late transfer tally add: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close tally storage: %v", err)
	}

	reopenedStorage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen tally storage: %v", err)
	}
	t.Cleanup(func() { _ = reopenedStorage.Close() })
	reopened, err := transfertally.Open(reopenedStorage)
	if err != nil {
		t.Fatalf("reopen tally: %v", err)
	}
	totals, err := reopened.Totals(ctx)
	if err != nil {
		t.Fatalf("read reopened tally: %v", err)
	}
	if totals.ReceivedWords != 7 {
		t.Fatalf("received words after drain = %d, want 7", totals.ReceivedWords)
	}
}
