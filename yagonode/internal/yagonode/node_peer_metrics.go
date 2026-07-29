package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

// knownPeerSamplingInterval rations the roster count behind the known-peer gauge.
// The count opens a read transaction on every shard, while the roster is touched on
// every peer exchange and every foreground read makes concurrent writers yield for
// the whole read-defer budget. Prometheus never scrapes faster than this, so a
// sample per interval is as fresh as the gauge can be observed anyway.
const knownPeerSamplingInterval = 5 * time.Second

var errPeerObservationsUnavailable = errors.New("peer observations unavailable")

type peerMetricsObserver interface {
	ObservePeerRoster(known, active int)
}

type observedPeerRoster struct {
	peerroster.Roster
	directory peerroster.Directory
	observer  peerMetricsObserver
	sampler   *knownPeerSampler
}

// knownPeerSampler holds the last roster count reported to the gauge. It lives
// behind a pointer because observedPeerRoster travels by value: every copy has to
// share one sample, or each would read the vault on its own schedule.
type knownPeerSampler struct {
	clock      func() time.Time
	mutex      sync.Mutex
	sampledAt  time.Time
	knownPeers int
}

type observedKnownPeerCounter interface {
	ObservedKnownPeerCount(ctx context.Context) (int, error)
}

func observePeerRoster(
	ctx context.Context,
	roster peerroster.Roster,
	observer peerMetricsObserver,
) peerroster.Roster {
	return observePeerRosterWithClock(ctx, roster, observer, time.Now)
}

// observePeerRosterWithClock is the sampling-clock seam so tests can drive the window.
func observePeerRosterWithClock(
	ctx context.Context,
	roster peerroster.Roster,
	observer peerMetricsObserver,
	clock func() time.Time,
) peerroster.Roster {
	if observer == nil {
		return roster
	}

	directory, _ := roster.(peerroster.Directory)
	observed := observedPeerRoster{
		Roster:    roster,
		directory: directory,
		observer:  observer,
		sampler:   &knownPeerSampler{clock: clock},
	}
	// Sample eagerly so the gauge reports the roster inherited from disk at boot
	// instead of zero until the first peer exchange.
	observed.observe(ctx)

	return observed
}

func (r observedPeerRoster) Discover(ctx context.Context, seeds ...yagomodel.Seed) {
	r.Roster.Discover(ctx, seeds...)
	r.observe(ctx)
}

func (r observedPeerRoster) ObserveCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
) {
	r.Roster.ObserveCaller(ctx, caller, classification)
	r.observe(ctx)
}

func (r observedPeerRoster) ObserveResponder(
	ctx context.Context,
	responder yagomodel.Seed,
) {
	r.Roster.ObserveResponder(ctx, responder)
	r.observe(ctx)
}

func (r observedPeerRoster) ObservePotential(
	ctx context.Context,
	potential yagomodel.Seed,
) {
	observer, ok := r.Roster.(interface {
		ObservePotential(context.Context, yagomodel.Seed)
	})
	if !ok {
		return
	}
	observer.ObservePotential(ctx, potential)
	r.observe(ctx)
}

func (r observedPeerRoster) ConfirmReachable(ctx context.Context, peer yagomodel.Hash) {
	r.Roster.ConfirmReachable(ctx, peer)
	r.observe(ctx)
}

func (r observedPeerRoster) ConfirmUnreachable(ctx context.Context, peer yagomodel.Hash) {
	r.Roster.ConfirmUnreachable(ctx, peer)
	r.observe(ctx)
}

func (r observedPeerRoster) PeerObservations(
	ctx context.Context,
) ([]peerroster.PeerObservation, int, int, error) {
	reader, ok := r.Roster.(peerroster.ObservationReader)
	if !ok {
		return nil, 0, 0, errPeerObservationsUnavailable
	}

	observations, known, reachable, err := reader.PeerObservations(ctx)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read peer observations: %w", err)
	}

	return observations, known, reachable, nil
}

func (r observedPeerRoster) PeerObservation(
	ctx context.Context,
	peer yagomodel.Hash,
) (peerroster.PeerObservation, bool, error) {
	reader, ok := r.Roster.(peerroster.ObservationReader)
	if !ok {
		return peerroster.PeerObservation{}, false, errPeerObservationsUnavailable
	}

	observation, found, err := reader.PeerObservation(ctx, peer)
	if err != nil {
		return peerroster.PeerObservation{}, false, fmt.Errorf(
			"read peer observation: %w",
			err,
		)
	}

	return observation, found, nil
}

func (r observedPeerRoster) ObservedKnownPeerCount(ctx context.Context) (int, error) {
	observed, ok := r.Roster.(observedKnownPeerCounter)
	if !ok {
		return r.KnownPeerCount(ctx), nil
	}
	count, err := observed.ObservedKnownPeerCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("count observed peers: %w", err)
	}

	return count, nil
}

func (r observedPeerRoster) observe(ctx context.Context) {
	// The reachable count is served from memory, so it stays exact on every call;
	// only the persisted known count is sampled.
	r.observer.ObservePeerRoster(
		r.knownPeerCount(ctx),
		r.ReachablePeerCount(ctx),
	)
}

// knownPeerCount routes the count through this roster's sampler. Only
// observePeerRosterWithClock installs one, so a struct literal assembled
// anywhere else used to fault here on its very first observation -- the
// sampler's mutex was taken through a nil pointer. A roster without one now
// counts through a private sampler: it cannot ration reads across copies of
// the roster, which is all the shared sampler buys, and it cannot crash.
func (r observedPeerRoster) knownPeerCount(ctx context.Context) int {
	sampler := r.sampler
	if sampler == nil {
		sampler = &knownPeerSampler{clock: time.Now}
	}

	return sampler.knownPeerCount(ctx, r.ObservedKnownPeerCount)
}

// knownPeerCount reports the newest sample, taking a fresh one only once the
// sampling interval has elapsed. Claiming the interval under the lock leaves at
// most one reader counting, so a slow read is never joined by a herd of
// identical ones; the read itself runs outside the lock because this is reached
// synchronously from the peer hello handler, whose budget is a second, and a
// mutex cannot honour a caller's deadline.
func (s *knownPeerSampler) knownPeerCount(
	ctx context.Context,
	count func(context.Context) (int, error),
) int {
	s.mutex.Lock()
	settled := s.knownPeers
	now := s.clock()
	if now.Before(s.sampledAt.Add(knownPeerSamplingInterval)) {
		s.mutex.Unlock()

		return settled
	}
	s.sampledAt = now
	s.mutex.Unlock()

	sampled, err := count(ctx)
	if err != nil {
		// A count fails when the read gives up under write pressure. Reporting zero
		// would make the gauge deny the roster exactly when it is asked to explain a
		// stall, so the last good sample stands until the next interval.
		slog.WarnContext(ctx, "known peer sample failed", slog.Any("error", err))

		return settled
	}
	s.mutex.Lock()
	s.knownPeers = sampled
	s.mutex.Unlock()

	return sampled
}
