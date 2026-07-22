package yagonode

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/searchremote"
)

const (
	nodePeerLifecycleCapacity       = 256
	nodePeerLifecycleWriteTimeout   = 250 * time.Millisecond
	nodePeerLifecycleDroppedMessage = "peer lifecycle observation dropped"
)

type nodePeerReachabilityState uint8

const (
	nodePeerReachabilityUnchanged nodePeerReachabilityState = iota
	nodePeerReachabilityFailed
	nodePeerReachabilitySucceeded
)

type nodePeerLifecyclePending struct {
	potential    *yagomodel.Seed
	reachability nodePeerReachabilityState
}

type nodePeerLifecycle struct {
	reachability searchremote.PeerReachability
	potential    documentsearch.PotentialPeerObserver
	queue        chan yagomodel.Hash
	mu           sync.Mutex
	pending      map[yagomodel.Hash]nodePeerLifecyclePending
	cancel       context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once
	closed       bool
}

func newNodePeerLifecycle(
	ctx context.Context,
	reachability searchremote.PeerReachability,
	potential documentsearch.PotentialPeerObserver,
) *nodePeerLifecycle {
	workerContext, cancel := context.WithCancel(ctx)
	lifecycle := &nodePeerLifecycle{
		reachability: reachability,
		potential:    potential,
		queue:        make(chan yagomodel.Hash, nodePeerLifecycleCapacity),
		pending:      make(map[yagomodel.Hash]nodePeerLifecyclePending),
		cancel:       cancel,
		done:         make(chan struct{}),
	}
	go lifecycle.run(workerContext)

	return lifecycle
}

func (l *nodePeerLifecycle) ConfirmReachable(ctx context.Context, peer yagomodel.Hash) {
	l.enqueue(ctx, peer, func(pending nodePeerLifecyclePending) nodePeerLifecyclePending {
		pending.reachability = nodePeerReachabilitySucceeded

		return pending
	})
}

func (l *nodePeerLifecycle) ConfirmUnreachable(ctx context.Context, peer yagomodel.Hash) {
	l.enqueue(ctx, peer, func(pending nodePeerLifecyclePending) nodePeerLifecyclePending {
		if pending.reachability != nodePeerReachabilitySucceeded {
			pending.reachability = nodePeerReachabilityFailed
		}

		return pending
	})
}

func (l *nodePeerLifecycle) ObservePotential(ctx context.Context, potential yagomodel.Seed) {
	detached := potential.Copy()
	l.enqueue(ctx, potential.Hash, func(pending nodePeerLifecyclePending) nodePeerLifecyclePending {
		pending.potential = &detached

		return pending
	})
}

func (l *nodePeerLifecycle) enqueue(
	ctx context.Context,
	peer yagomodel.Hash,
	merge func(nodePeerLifecyclePending) nodePeerLifecyclePending,
) {
	if peer == "" || context.Cause(ctx) != nil {
		return
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()

		return
	}
	pending, found := l.pending[peer]
	if !found && len(l.pending) == nodePeerLifecycleCapacity {
		l.mu.Unlock()
		slog.WarnContext(ctx, nodePeerLifecycleDroppedMessage,
			slog.String("reason", "observation queue full"))

		return
	}
	l.pending[peer] = merge(pending)
	if !found {
		l.queue <- peer
	}
	l.mu.Unlock()
}

func (l *nodePeerLifecycle) run(ctx context.Context) {
	defer func() {
		l.mu.Lock()
		l.closed = true
		l.pending = nil
		l.reachability = nil
		l.potential = nil
		l.mu.Unlock()
		close(l.done)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case peer := <-l.queue:
			pending, found := l.take(peer)
			if found {
				l.apply(ctx, peer, pending)
			}
		}
	}
}

func (l *nodePeerLifecycle) take(
	peer yagomodel.Hash,
) (nodePeerLifecyclePending, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	pending, found := l.pending[peer]
	delete(l.pending, peer)

	return pending, found
}

func (l *nodePeerLifecycle) apply(
	ctx context.Context,
	peer yagomodel.Hash,
	pending nodePeerLifecyclePending,
) {
	if pending.potential != nil && l.potential != nil {
		writeContext, cancel := context.WithTimeout(ctx, nodePeerLifecycleWriteTimeout)
		l.potential.ObservePotential(writeContext, *pending.potential)
		cancel()
	}
	writeContext, cancel := context.WithTimeout(ctx, nodePeerLifecycleWriteTimeout)
	defer cancel()
	switch pending.reachability {
	case nodePeerReachabilitySucceeded:
		l.reachability.ConfirmReachable(writeContext, peer)
	case nodePeerReachabilityFailed:
		l.reachability.ConfirmUnreachable(writeContext, peer)
	}
}

func (l *nodePeerLifecycle) Close() {
	if l == nil {
		return
	}
	l.closeOnce.Do(func() {
		l.cancel()
		<-l.done
	})
}
