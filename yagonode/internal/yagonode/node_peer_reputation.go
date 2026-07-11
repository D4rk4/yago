package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
)

const (
	peerReputationUpdateFailedMessage = "peer reputation update failed"
	peerReputationQueueCapacity       = 256
	peerReputationSnapshotRefresh     = 5 * time.Minute
	peerReputationMaximumAttempts     = 3
	peerReputationInitialRetryDelay   = 10 * time.Millisecond
	peerReputationRetryDelayExponent  = 6
	peerReputationShutdownWait        = 2 * time.Second
)

type peerReputationBatchLedger interface {
	LastBatchSequence(context.Context) (uint64, error)
	Snapshot(context.Context, time.Time) (peerreputation.Snapshot, error)
	ObserveBatch(
		context.Context,
		peerreputation.ObservationBatch,
	) (peerreputation.BatchApplication, error)
}

type peerReputationObserver struct {
	ledger       peerReputationBatchLedger
	sequence     atomic.Uint64
	snapshot     atomic.Pointer[peerreputation.Snapshot]
	queue        chan []peerreputation.Observation
	cancel       context.CancelFunc
	done         chan struct{}
	shutdownDone chan struct{}
	admission    sync.RWMutex
	shutdown     sync.Once
	closed       bool
	refresh      <-chan time.Time
	stopRefresh  func()
	shutdownWait time.Duration
}

func newPeerReputationObserver(
	ctx context.Context,
	ledger peerReputationBatchLedger,
) (*peerReputationObserver, error) {
	ticker := time.NewTicker(peerReputationSnapshotRefresh)
	observer, err := newPeerReputationObserverWithRefresh(
		ctx, ledger, ticker.C, ticker.Stop,
	)
	if err != nil {
		ticker.Stop()
	}

	return observer, err
}

func newPeerReputationObserverWithRefresh(
	ctx context.Context,
	ledger peerReputationBatchLedger,
	refresh <-chan time.Time,
	stopRefresh func(),
) (*peerReputationObserver, error) {
	last, err := ledger.LastBatchSequence(ctx)
	if err != nil {
		return nil, fmt.Errorf("read peer reputation sequence: %w", err)
	}
	if last == math.MaxUint64 {
		return nil, fmt.Errorf("peer reputation sequence is exhausted")
	}
	snapshot, err := ledger.Snapshot(ctx, time.Now())
	if err != nil {
		return nil, fmt.Errorf("read peer reputation snapshot: %w", err)
	}
	workerContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	observer := &peerReputationObserver{
		ledger: ledger, refresh: refresh, stopRefresh: stopRefresh,
		queue:        make(chan []peerreputation.Observation, peerReputationQueueCapacity),
		cancel:       cancel,
		done:         make(chan struct{}),
		shutdownDone: make(chan struct{}),
		shutdownWait: peerReputationShutdownWait,
	}
	observer.sequence.Store(last + 1)
	observer.snapshot.Store(&snapshot)
	go observer.run(workerContext)

	return observer, nil
}

func (observer *peerReputationObserver) Observe(
	ctx context.Context,
	observations []peerreputation.Observation,
) {
	if observer == nil || len(observations) == 0 {
		return
	}
	observer.admission.RLock()
	defer observer.admission.RUnlock()
	if observer.closed {
		return
	}
	batch := slices.Clone(observations)
	select {
	case observer.queue <- batch:
	default:
		slog.WarnContext(
			ctx,
			peerReputationUpdateFailedMessage,
			slog.String("reason", "observation queue full"),
		)
	}
}

func (observer *peerReputationObserver) run(ctx context.Context) {
	defer close(observer.done)
	for {
		select {
		case <-ctx.Done():
			return
		case observations, open := <-observer.queue:
			if !open {
				return
			}
			observer.persist(ctx, observations)
		case at := <-observer.refresh:
			observer.refreshSnapshot(ctx, at)
		}
	}
}

func (observer *peerReputationObserver) persist(
	ctx context.Context,
	observations []peerreputation.Observation,
) {
	sequence := observer.sequence.Load()
	if sequence == 0 {
		slog.WarnContext(
			ctx,
			peerReputationUpdateFailedMessage,
			slog.String("reason", "sequence exhausted"),
		)

		return
	}
	for attempt := 0; ; attempt++ {
		application, err := observer.ledger.ObserveBatch(ctx, peerreputation.ObservationBatch{
			Sequence: sequence, Observations: observations,
		})
		if err == nil {
			if application.Superseded {
				if application.LastSequence == math.MaxUint64 {
					observer.sequence.Store(0)
					slog.WarnContext(
						ctx,
						peerReputationUpdateFailedMessage,
						slog.String("reason", "sequence exhausted"),
					)

					return
				}
				sequence = application.LastSequence + 1
				observer.sequence.Store(sequence)
				attempt = -1

				continue
			}
			lastSequence := max(sequence, application.LastSequence)
			if lastSequence == math.MaxUint64 {
				observer.sequence.Store(0)
			} else {
				observer.sequence.Store(lastSequence + 1)
			}
			observer.refreshSnapshot(ctx, latestPeerObservation(observations))

			return
		}
		if attempt == peerReputationMaximumAttempts-1 {
			slog.WarnContext(ctx, peerReputationUpdateFailedMessage, slog.Any("error", err))
		}
		if !waitPeerReputationRetry(ctx, attempt) {
			return
		}
	}
}

func waitPeerReputationRetry(ctx context.Context, attempt int) bool {
	delay := peerReputationInitialRetryDelay << min(
		attempt,
		peerReputationRetryDelayExponent,
	)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (observer *peerReputationObserver) refreshSnapshot(ctx context.Context, at time.Time) {
	snapshot, err := observer.ledger.Snapshot(ctx, at)
	if err != nil {
		slog.WarnContext(ctx, peerReputationUpdateFailedMessage, slog.Any("error", err))

		return
	}
	observer.snapshot.Store(&snapshot)
}

func (observer *peerReputationObserver) Snapshot(
	context.Context,
	time.Time,
) (peerreputation.Snapshot, error) {
	return *observer.snapshot.Load(), nil
}

func (observer *peerReputationObserver) Close() {
	if observer == nil {
		return
	}
	observer.shutdown.Do(func() {
		observer.admission.Lock()
		observer.closed = true
		observer.stopRefresh()
		close(observer.queue)
		observer.admission.Unlock()
		timer := time.NewTimer(observer.shutdownWait)
		defer timer.Stop()
		select {
		case <-observer.done:
		case <-timer.C:
		}
		observer.cancel()
		close(observer.shutdownDone)
	})
	<-observer.shutdownDone
}

func stopPeerReputation(cancel context.CancelFunc, observer *peerReputationObserver) {
	observer.Close()
	cancel()
}

func latestPeerObservation(observations []peerreputation.Observation) time.Time {
	latest := observations[0].ObservedAt
	for _, observation := range observations[1:] {
		if observation.ObservedAt.After(latest) {
			latest = observation.ObservedAt
		}
	}

	return latest
}

func peerReputationNetworkGroup(seed yagomodel.Seed) peerreputation.NetworkGroupKey {
	address, ok := seed.NetworkAddress()
	if !ok {
		return ""
	}
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil {
		return "hostname:unresolved"
	}
	ip := endpoint.Addr().Unmap().WithZone("")
	prefixLength := 48
	prefixKind := "ipv6:"
	if ip.Is4() {
		prefixLength = 24
		prefixKind = "ipv4:"
	}

	return peerreputation.NetworkGroupKey(
		prefixKind + netip.PrefixFrom(ip, prefixLength).Masked().String(),
	)
}
