package crawlbroker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestWorkerSessionRegistryRejectsLiveTakeoverWithoutMutation(t *testing.T) {
	registry := newWorkerSessionRegistry(2)
	var cancelled atomic.Bool
	firstGeneration, err := registry.activate(
		"worker",
		"session-a",
		func() { cancelled.Store(true) },
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate first session: %v", err)
	}
	for _, session := range []string{"session-a", "session-b"} {
		adopted := false
		if _, err := registry.activate("worker", session, func() {}, func() error {
			adopted = true

			return nil
		}); !errors.Is(err, errWorkerSessionActive) {
			t.Fatalf("activate live %s error = %v", session, err)
		}
		if adopted {
			t.Fatalf("live %s attempted lease adoption", session)
		}
	}
	current := registry.registration("worker")
	if current.id != "session-a" || current.generation != firstGeneration ||
		!current.connected || cancelled.Load() {
		t.Fatalf("session after takeover attempts = %+v, cancelled=%v", current, cancelled.Load())
	}
}

func TestWorkerSessionAdoptionFailurePreservesDisconnectedRegistration(t *testing.T) {
	registry := newWorkerSessionRegistry(2)
	firstGeneration, err := registry.activate(
		"worker",
		"session-a",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate first session: %v", err)
	}
	registry.deactivate("worker", "session-a", firstGeneration)
	wantErr := errors.New("adoption failed")
	if _, err := registry.activate("worker", "session-b", func() {}, func() error {
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("failed adoption error = %v", err)
	}
	current := registry.registration("worker")
	if current.id != "session-a" || current.generation != firstGeneration || current.connected {
		t.Fatalf("registration after failed adoption = %+v", current)
	}
	secondGeneration, err := registry.activate(
		"worker",
		"session-b",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("retry adoption: %v", err)
	}
	registry.deactivate("worker", "session-a", firstGeneration)
	current = registry.registration("worker")
	if current.id != "session-b" || current.generation != secondGeneration || !current.connected {
		t.Fatalf("registration after stale release = %+v", current)
	}
}

func TestConcurrentWorkerSessionActivationHasSingleWinner(t *testing.T) {
	registry := newWorkerSessionRegistry(2)
	start := make(chan struct{})
	type result struct {
		session    string
		generation uint64
		err        error
	}
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, session := range []string{"session-a", "session-b"} {
		go func() {
			ready.Done()
			<-start
			generation, err := registry.activate(
				"worker",
				session,
				func() {},
				func() error { return nil },
			)
			results <- result{session: session, generation: generation, err: err}
		}()
	}
	ready.Wait()
	close(start)
	first := <-results
	second := <-results
	successes := 0
	conflicts := 0
	for _, activation := range []result{first, second} {
		switch {
		case activation.err == nil && activation.generation != 0:
			successes++
			current := registry.registration("worker")
			if current.id != activation.session || current.generation != activation.generation {
				t.Fatalf("winning activation = %+v, current=%+v", activation, current)
			}
		case errors.Is(activation.err, errWorkerSessionActive) && activation.generation == 0:
			conflicts++
		default:
			t.Fatalf("activation result = %+v", activation)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("activation outcomes success=%d conflict=%d", successes, conflicts)
	}
}
