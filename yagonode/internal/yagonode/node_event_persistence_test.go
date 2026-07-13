package yagonode

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/events"
)

type eventAppenderFunc func(context.Context, events.Event) error

func (appendEvent eventAppenderFunc) Append(ctx context.Context, event events.Event) error {
	return appendEvent(ctx, event)
}

type capturedEventAppender struct {
	mu      sync.Mutex
	events  []events.Event
	started chan struct{}
	gate    <-chan struct{}
}

func (a *capturedEventAppender) Append(
	ctx context.Context,
	event events.Event,
) error {
	a.mu.Lock()
	a.events = append(a.events, event)
	a.mu.Unlock()
	if a.started != nil {
		select {
		case a.started <- struct{}{}:
		default:
		}
	}
	if a.gate != nil {
		select {
		case <-a.gate:
		case <-ctx.Done():
			return fmt.Errorf("append event: %w", ctx.Err())
		}
	}

	return nil
}

func (a *capturedEventAppender) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return len(a.events)
}

func TestEventPersistenceNeverBlocksRecorderAndBoundsPendingEvents(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	appender := &capturedEventAppender{started: started, gate: gate}
	persistence := newEventPersistence(appender)
	persistence.Persist(events.Event{Name: "active"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("event append did not start")
	}
	for i := range eventPersistenceCapacity {
		persistence.Persist(events.Event{Name: fmt.Sprintf("queued-%d", i)})
	}
	returned := make(chan struct{})
	go func() {
		persistence.Persist(events.Event{Name: "overflow"})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("full persistence queue blocked the recorder")
	}
	if len(persistence.queue) != eventPersistenceCapacity {
		t.Fatalf("pending events = %d, want %d", len(persistence.queue), eventPersistenceCapacity)
	}
	close(gate)
	if err := persistence.Close(t.Context()); err != nil {
		t.Fatalf("close persistence: %v", err)
	}
	if got := appender.count(); got != eventPersistenceCapacity+1 {
		t.Fatalf("persisted events = %d, want %d", got, eventPersistenceCapacity+1)
	}
	persistence.Persist(events.Event{Name: "after-close"})
	if err := persistence.Close(t.Context()); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestEventPersistenceCloseDeadlineCancelsBlockedAppend(t *testing.T) {
	started := make(chan struct{}, 1)
	persistence := newEventPersistence(eventAppenderFunc(func(
		ctx context.Context,
		_ events.Event,
	) error {
		started <- struct{}{}
		<-ctx.Done()

		return ctx.Err()
	}))
	persistence.Persist(events.Event{Name: "blocked"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocked append did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := persistence.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline", err)
	}
}

type contextIgnoringEventAppender struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
	active  atomic.Bool
}

func (a *contextIgnoringEventAppender) Append(context.Context, events.Event) error {
	a.calls.Add(1)
	a.active.Store(true)
	select {
	case a.started <- struct{}{}:
	default:
	}
	<-a.release
	a.active.Store(false)

	return nil
}

func TestEventPersistenceDeadlineBoundsContextIgnoringAppender(t *testing.T) {
	appender := &contextIgnoringEventAppender{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	persistence := newEventPersistence(appender)
	persistence.Persist(events.Event{Name: "blocked"})
	<-appender.started
	persistence.Persist(events.Event{Name: "must-not-start"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := persistence.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("deadline close elapsed = %v", elapsed)
	}
	select {
	case <-persistence.done:
		t.Fatal("worker exited while appender remained active")
	default:
	}
	close(appender.release)
	select {
	case <-persistence.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after appender release")
	}
	if calls := appender.calls.Load(); calls != 1 {
		t.Fatalf("append calls = %d, want 1", calls)
	}
}

type eventDrainVaultCloser struct {
	active *atomic.Bool
	closed chan bool
}

func (c eventDrainVaultCloser) Close() error {
	c.closed <- c.active.Load()

	return nil
}

func TestVaultCloseWaitsForEventPersistenceDrain(t *testing.T) {
	appender := &contextIgnoringEventAppender{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	persistence := newEventPersistence(appender)
	persistence.Persist(events.Event{Name: "blocked"})
	<-appender.started
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := persistence.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline", err)
	}
	closed := make(chan bool, 1)
	closeVaultAfterEventDrain(
		eventDrainVaultCloser{active: &appender.active, closed: closed},
		persistence.done,
	)
	select {
	case <-closed:
		t.Fatal("vault closed before event persistence drained")
	default:
	}
	close(appender.release)
	select {
	case wasActive := <-closed:
		if wasActive {
			t.Fatal("vault closed while event append remained active")
		}
	case <-time.After(time.Second):
		t.Fatal("vault did not close after event persistence drained")
	}
}

func TestVaultCloseHandlesAbsentAndCompletedEventDrain(t *testing.T) {
	active := &atomic.Bool{}
	closed := make(chan bool, 2)
	closer := eventDrainVaultCloser{active: active, closed: closed}
	closeVaultAfterEventDrain(closer, nil)
	done := make(chan struct{})
	close(done)
	closeVaultAfterEventDrain(closer, done)
	for range 2 {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("vault did not close synchronously")
		}
	}
}

func TestEventPersistenceWorkerCanStopWhileIdle(t *testing.T) {
	persistence := newEventPersistence(eventAppenderFunc(func(
		context.Context,
		events.Event,
	) error {
		return nil
	}))
	persistence.cancel()
	select {
	case <-persistence.done:
	case <-time.After(time.Second):
		t.Fatal("idle persistence worker did not stop")
	}
	if err := persistence.Close(t.Context()); err != nil {
		t.Fatalf("close stopped persistence: %v", err)
	}
}

type eventCancellationRaceContext struct {
	context.Context
}

func (eventCancellationRaceContext) Err() error {
	return context.Canceled
}

func TestEventPersistenceStopsWhenCancellationFollowsQueueSelection(t *testing.T) {
	called := false
	persistence := &eventPersistence{
		appender: eventAppenderFunc(func(context.Context, events.Event) error {
			called = true

			return nil
		}),
		queue: make(chan events.Event, 1),
		done:  make(chan struct{}),
	}
	persistence.queue <- events.Event{Name: "cancelled"}
	persistence.run(eventCancellationRaceContext{Context: context.Background()})
	if called {
		t.Fatal("event appended after worker cancellation")
	}
}

func TestCloseEventPersistenceHandlesDrainDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	persistence := newEventPersistence(eventAppenderFunc(func(
		ctx context.Context,
		_ events.Event,
	) error {
		started <- struct{}{}
		<-ctx.Done()

		return ctx.Err()
	}))
	persistence.Persist(events.Event{Name: "blocked"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocked append did not start")
	}
	closeEventPersistenceWithin(persistence, time.Millisecond)
}
