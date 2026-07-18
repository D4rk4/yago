package boltvault

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type observedDoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })

	return c.Context.Done()
}

type controlledCancellationContext struct {
	context.Context
	done     chan struct{}
	cancelAt int32
	calls    atomic.Int32
	once     sync.Once
}

func (c *controlledCancellationContext) Done() <-chan struct{} {
	return c.done
}

func (c *controlledCancellationContext) Err() error {
	if c.calls.Add(1) < c.cancelAt {
		return nil
	}
	c.once.Do(func() { close(c.done) })

	return context.Canceled
}

func newControlledCancellationContext(cancelAt int32) *controlledCancellationContext {
	return &controlledCancellationContext{
		Context:  context.Background(),
		done:     make(chan struct{}),
		cancelAt: cancelAt,
	}
}

func TestWriterAdmissionRechecksCancellationAfterToken(t *testing.T) {
	var admission writerAdmission
	if err := admission.acquire(
		newControlledCancellationContext(1),
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("admission error = %v, want context.Canceled", err)
	}
	if err := admission.acquire(t.Context()); err != nil {
		t.Fatalf("canceled admission retained writer token: %v", err)
	}
	admission.release()
}

func TestUpdateRechecksCancellationBeforeCallback(t *testing.T) {
	storage := &engine{db: openTestBolt(t)}
	var callbackRan atomic.Bool
	err := storage.Update(newControlledCancellationContext(2), func(vault.EngineTxn) error {
		callbackRan.Store(true)

		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("update error = %v, want context.Canceled", err)
	}
	if callbackRan.Load() {
		t.Fatal("update ran callback after cancellation")
	}
}

func TestUpdateCanceledWhileWaitingDoesNotRunCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	assertWaitingUpdateDoesNotRun(t, ctx, cancel, context.Canceled)
}

func TestUpdateTimedOutWhileWaitingDoesNotRunCallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	assertWaitingUpdateDoesNotRun(t, ctx, func() {}, context.DeadlineExceeded)
}

func assertWaitingUpdateDoesNotRun(
	t *testing.T,
	ctx context.Context,
	endContext func(),
	want error,
) {
	t.Helper()
	storage := &engine{db: openTestBolt(t)}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- storage.Update(t.Context(), func(vault.EngineTxn) error {
			close(firstEntered)
			<-releaseFirst

			return nil
		})
	}()
	<-firstEntered

	observed := make(chan struct{})
	waitingContext := &observedDoneContext{Context: ctx, observed: observed}
	var callbackRan atomic.Bool
	waitingResult := make(chan error, 1)
	go func() {
		waitingResult <- storage.Update(waitingContext, func(vault.EngineTxn) error {
			callbackRan.Store(true)

			return nil
		})
	}()
	select {
	case <-observed:
	case <-time.After(time.Second):
		close(releaseFirst)
		<-firstResult
		t.Fatal("waiting update did not observe its context")
	}
	endContext()
	select {
	case err := <-waitingResult:
		if !errors.Is(err, want) {
			close(releaseFirst)
			<-firstResult
			t.Fatalf("waiting update error = %v, want %v", err, want)
		}
	case <-time.After(time.Second):
		close(releaseFirst)
		firstErr := <-firstResult
		waitingErr := <-waitingResult
		t.Fatalf(
			"waiting update remained blocked: first=%v waiting=%v callback=%t",
			firstErr,
			waitingErr,
			callbackRan.Load(),
		)
	}
	if callbackRan.Load() {
		close(releaseFirst)
		<-firstResult
		t.Fatal("waiting update ran its callback after context completion")
	}
	close(releaseFirst)
	if err := <-firstResult; err != nil {
		t.Fatalf("first update failed: %v", err)
	}
	if callbackRan.Load() {
		t.Fatal("waiting update ran its callback after the writer was released")
	}
}

func TestUpdateCancellationAfterCallbackRollsBack(t *testing.T) {
	storage := &engine{db: openTestBolt(t)}
	ctx, cancel := context.WithCancel(t.Context())
	callbackApplied := make(chan struct{})
	finishCallback := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- storage.Update(ctx, func(tx vault.EngineTxn) error {
			if err := tx.Bucket("bucket").Put(vault.Key("cancelled"), []byte("value")); err != nil {
				return fmt.Errorf("put cancelled value: %w", err)
			}
			close(callbackApplied)
			<-finishCallback

			return nil
		})
	}()
	<-callbackApplied
	cancel()
	close(finishCallback)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled update error = %v, want context.Canceled", err)
	}
	if err := storage.View(t.Context(), func(tx vault.EngineTxn) error {
		if value := tx.Bucket("bucket").Get(vault.Key("cancelled")); value != nil {
			t.Fatalf("cancelled update committed value %q", value)
		}

		return nil
	}); err != nil {
		t.Fatalf("inspect cancelled update: %v", err)
	}
}
