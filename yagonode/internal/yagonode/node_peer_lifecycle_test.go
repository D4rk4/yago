package yagonode

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type controlledNodePeerLifecycleSink struct {
	started chan string
	release chan struct{}
	once    sync.Once
}

type contextBoundedNodePeerLifecycleSink struct {
	started chan struct{}
}

type firstWriteBlockingNodePeerLifecycleSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *firstWriteBlockingNodePeerLifecycleSink) ConfirmReachable(
	ctx context.Context,
	_ yagomodel.Hash,
) {
	s.once.Do(func() {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	})
}

func (s *firstWriteBlockingNodePeerLifecycleSink) ConfirmUnreachable(
	ctx context.Context,
	peer yagomodel.Hash,
) {
	s.ConfirmReachable(ctx, peer)
}

func (s *contextBoundedNodePeerLifecycleSink) ConfirmReachable(
	ctx context.Context,
	_ yagomodel.Hash,
) {
	close(s.started)
	<-ctx.Done()
}

func (s *contextBoundedNodePeerLifecycleSink) ConfirmUnreachable(
	ctx context.Context,
	peer yagomodel.Hash,
) {
	s.ConfirmReachable(ctx, peer)
}

func (s *controlledNodePeerLifecycleSink) ConfirmReachable(
	ctx context.Context,
	_ yagomodel.Hash,
) {
	s.observe(ctx, "reachable")
}

func (s *controlledNodePeerLifecycleSink) ConfirmUnreachable(
	ctx context.Context,
	_ yagomodel.Hash,
) {
	s.observe(ctx, "unreachable")
}

func (s *controlledNodePeerLifecycleSink) ObservePotential(
	ctx context.Context,
	_ yagomodel.Seed,
) {
	s.observe(ctx, "potential")
}

func (s *controlledNodePeerLifecycleSink) observe(ctx context.Context, event string) {
	s.started <- event
	s.once.Do(func() {
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	})
}

func TestNodePeerLifecycleOrdersAndCoalescesPeerState(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	sink := &controlledNodePeerLifecycleSink{
		started: make(chan string, 4),
		release: make(chan struct{}),
	}
	lifecycle := newNodePeerLifecycle(ctx, sink, sink)
	peer := yagomodel.Hash("AAAAAAAAAAAA")
	lifecycle.ConfirmUnreachable(t.Context(), peer)
	if event := <-sink.started; event != "unreachable" {
		t.Fatalf("first lifecycle event = %q", event)
	}

	begin := time.Now()
	lifecycle.ConfirmReachable(t.Context(), peer)
	lifecycle.ConfirmUnreachable(t.Context(), peer)
	if elapsed := time.Since(begin); elapsed >= 50*time.Millisecond {
		t.Fatalf("lifecycle enqueue took %s", elapsed)
	}
	close(sink.release)
	select {
	case event := <-sink.started:
		if event != "reachable" {
			t.Fatalf("coalesced lifecycle event = %q", event)
		}
	case <-time.After(time.Second):
		t.Fatal("coalesced lifecycle event was not delivered")
	}
	select {
	case event := <-sink.started:
		t.Fatalf("unexpected intermediate lifecycle event = %q", event)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	lifecycle.Close()
	lifecycle.mu.Lock()
	closed := lifecycle.closed
	reachability := lifecycle.reachability
	potential := lifecycle.potential
	pending := lifecycle.pending
	lifecycle.mu.Unlock()
	if !closed || reachability != nil || potential != nil || pending != nil {
		t.Fatalf("closed lifecycle retained state = %t/%T/%T/%v",
			closed, reachability, potential, pending)
	}
}

func TestNodePeerLifecyclePotentialObservationNeverBlocksRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	sink := &controlledNodePeerLifecycleSink{
		started: make(chan string, 1),
		release: make(chan struct{}),
	}
	lifecycle := newNodePeerLifecycle(ctx, sink, sink)
	begin := time.Now()
	lifecycle.ObservePotential(t.Context(), yagomodel.Seed{
		Hash: yagomodel.Hash("AAAAAAAAAAAA"),
	})
	if elapsed := time.Since(begin); elapsed >= 50*time.Millisecond {
		t.Fatalf("potential observation enqueue took %s", elapsed)
	}
	select {
	case event := <-sink.started:
		if event != "potential" {
			t.Fatalf("potential lifecycle event = %q", event)
		}
	case <-time.After(time.Second):
		t.Fatal("potential lifecycle event was not dispatched")
	}
	cancel()
	lifecycle.Close()
}

func TestNodePeerLifecycleCloseCancelsInFlightWrite(t *testing.T) {
	sink := &contextBoundedNodePeerLifecycleSink{started: make(chan struct{})}
	lifecycle := newNodePeerLifecycle(t.Context(), sink, nil)
	lifecycle.ConfirmReachable(t.Context(), yagomodel.Hash("AAAAAAAAAAAA"))
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("lifecycle write did not start")
	}
	closed := make(chan struct{})
	go func() {
		lifecycle.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("lifecycle close waited for the write timeout")
	}
}

func TestNodePeerLifecycleRejectsInvalidAndClosedObservations(t *testing.T) {
	sink := &controlledNodePeerLifecycleSink{
		started: make(chan string, 1),
		release: make(chan struct{}),
	}
	lifecycle := newNodePeerLifecycle(t.Context(), sink, sink)
	lifecycle.ConfirmReachable(t.Context(), "")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	lifecycle.ConfirmReachable(canceled, yagomodel.Hash("AAAAAAAAAAAA"))
	lifecycle.Close()
	lifecycle.ConfirmReachable(t.Context(), yagomodel.Hash("BBBBBBBBBBBB"))

	select {
	case event := <-sink.started:
		t.Fatalf("rejected lifecycle observation was delivered as %q", event)
	default:
	}
}

func TestNodePeerLifecycleBoundsPendingObservations(t *testing.T) {
	sink := &firstWriteBlockingNodePeerLifecycleSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	lifecycle := newNodePeerLifecycle(t.Context(), sink, nil)
	lifecycle.ConfirmReachable(t.Context(), yagomodel.Hash("FIRSTPEER001"))
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("first lifecycle write did not start")
	}
	for observation := range nodePeerLifecycleCapacity {
		lifecycle.ConfirmReachable(
			t.Context(),
			yagomodel.Hash(fmt.Sprintf("%012d", observation)),
		)
	}
	begin := time.Now()
	lifecycle.ConfirmReachable(t.Context(), yagomodel.Hash("OVERFLOW0001"))
	if elapsed := time.Since(begin); elapsed >= 50*time.Millisecond {
		t.Fatalf("overflow observation blocked for %s", elapsed)
	}
	lifecycle.mu.Lock()
	pending := len(lifecycle.pending)
	lifecycle.mu.Unlock()
	if pending != nodePeerLifecycleCapacity {
		t.Fatalf("pending observations = %d, want %d", pending, nodePeerLifecycleCapacity)
	}
	close(sink.release)
	lifecycle.Close()
}
