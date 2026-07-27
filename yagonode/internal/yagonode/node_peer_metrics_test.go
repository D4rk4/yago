package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

type countingPeerRoster struct {
	known     int
	reachable int
}

func (r *countingPeerRoster) Discover(_ context.Context, seeds ...yagomodel.Seed) {
	r.known += len(seeds)
}

func (r *countingPeerRoster) ObserveCaller(
	context.Context,
	yagomodel.Seed,
	yagomodel.PeerType,
) {
	r.known++
}

func (r *countingPeerRoster) ObserveResponder(context.Context, yagomodel.Seed) {
	r.known++
	r.reachable++
}

func (r *countingPeerRoster) ConfirmReachable(context.Context, yagomodel.Hash) {
	r.reachable++
}

func (r *countingPeerRoster) ConfirmUnreachable(context.Context, yagomodel.Hash) {
	r.known--
	r.reachable--
}

func (r *countingPeerRoster) RejectRemoteIndex(context.Context, yagomodel.Seed) {}

func (r *countingPeerRoster) FreshestPeers(context.Context, int) []yagomodel.Seed { return nil }

func (r *countingPeerRoster) ReachablePeers(context.Context) []yagomodel.Seed { return nil }

func (r *countingPeerRoster) PeerByHash(
	context.Context,
	yagomodel.Hash,
) (yagomodel.Seed, bool) {
	return yagomodel.Seed{}, false
}

func (r *countingPeerRoster) KnownPeerCount(context.Context) int {
	return r.known
}

func (r *countingPeerRoster) ReachablePeerCount(context.Context) int {
	return r.reachable
}

type recordingPeerMetrics struct {
	calls     int
	lastKnown int
	lastLive  int
}

// peerRosterClock drives the known-peer sampling window from the test instead of
// waiting out real seconds.
type peerRosterClock struct {
	now time.Time
}

func newPeerRosterClock() *peerRosterClock {
	return &peerRosterClock{now: time.Unix(1_000_000, 0)}
}

func (c *peerRosterClock) Now() time.Time { return c.now }

func (c *peerRosterClock) advance(step time.Duration) { c.now = c.now.Add(step) }

// persistedPeerCountRoster records how often the persisted roster size was read,
// which is the all-shard read transaction the sampler exists to ration.
type persistedPeerCountRoster struct {
	countingPeerRoster
	persistedReads int
	err            error
}

func (r *persistedPeerCountRoster) ObservedKnownPeerCount(context.Context) (int, error) {
	r.persistedReads++
	if r.err != nil {
		return 0, r.err
	}

	return r.known, nil
}

type observationCountingRoster struct {
	countingPeerRoster
	observation peerroster.PeerObservation
	err         error
}

func (r *observationCountingRoster) PeerObservations(
	context.Context,
) ([]peerroster.PeerObservation, int, int, error) {
	if r.err != nil {
		return nil, 0, 0, r.err
	}

	return []peerroster.PeerObservation{r.observation}, 1, 0, nil
}

func (r *observationCountingRoster) PeerObservation(
	context.Context,
	yagomodel.Hash,
) (peerroster.PeerObservation, bool, error) {
	if r.err != nil {
		return peerroster.PeerObservation{}, false, r.err
	}

	return r.observation, true, nil
}

func (r *observationCountingRoster) ObservedKnownPeerCount(context.Context) (int, error) {
	if r.err != nil {
		return 0, r.err
	}

	return r.known, nil
}

func (m *recordingPeerMetrics) ObservePeerRoster(known, active int) {
	m.calls++
	m.lastKnown = known
	m.lastLive = active
}

func TestObservePeerRosterReturnsOriginalWithoutObserver(t *testing.T) {
	roster := &countingPeerRoster{}

	if got := observePeerRoster(context.Background(), roster, nil); got != roster {
		t.Fatalf("roster = %T, want original", got)
	}
}

func TestObservedPeerRosterUpdatesCounts(t *testing.T) {
	ctx := context.Background()
	clock := newPeerRosterClock()
	observer := &recordingPeerMetrics{}
	roster := observePeerRosterWithClock(ctx, &countingPeerRoster{}, observer, clock.Now)

	if observer.calls != 1 {
		t.Fatalf("initial observations = %d, want 1", observer.calls)
	}

	roster.Discover(ctx, yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")})
	clock.advance(knownPeerSamplingInterval)
	roster.ConfirmReachable(ctx, yagomodel.Hash("AAAAAAAAAAAA"))
	if observer.lastKnown != 1 || observer.lastLive != 1 {
		t.Fatalf(
			"observed counts = %d/%d, want 1/1",
			observer.lastKnown,
			observer.lastLive,
		)
	}

	clock.advance(knownPeerSamplingInterval)
	roster.ConfirmUnreachable(ctx, yagomodel.Hash("AAAAAAAAAAAA"))
	if observer.lastKnown != 0 || observer.lastLive != 0 {
		t.Fatalf(
			"observed counts after drop = %d/%d, want 0/0",
			observer.lastKnown,
			observer.lastLive,
		)
	}
}

func TestObservedPeerRosterUpdatesCountsForCallerObservation(t *testing.T) {
	clock := newPeerRosterClock()
	observer := &recordingPeerMetrics{}
	roster := observePeerRosterWithClock(
		t.Context(), &countingPeerRoster{}, observer, clock.Now,
	)
	clock.advance(knownPeerSamplingInterval)

	roster.ObserveCaller(
		t.Context(),
		yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")},
		yagomodel.PeerJunior,
	)

	if observer.lastKnown != 1 || observer.lastLive != 0 {
		t.Fatalf(
			"observed caller counts = %d/%d, want 1/0",
			observer.lastKnown,
			observer.lastLive,
		)
	}
}

func TestObservedPeerRosterUpdatesCountsForResponderObservation(t *testing.T) {
	clock := newPeerRosterClock()
	observer := &recordingPeerMetrics{}
	roster := observePeerRosterWithClock(
		t.Context(), &countingPeerRoster{}, observer, clock.Now,
	)
	clock.advance(knownPeerSamplingInterval)

	roster.ObserveResponder(
		t.Context(),
		yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")},
	)

	if observer.lastKnown != 1 || observer.lastLive != 1 {
		t.Fatalf(
			"observed responder counts = %d/%d, want 1/1",
			observer.lastKnown,
			observer.lastLive,
		)
	}
}

func TestObservedPeerRosterReadsPersistedPeerCountOncePerSamplingInterval(t *testing.T) {
	clock := newPeerRosterClock()
	base := &persistedPeerCountRoster{}
	observer := &recordingPeerMetrics{}
	roster := observePeerRosterWithClock(t.Context(), base, observer, clock.Now)

	base.known = 7
	clock.advance(knownPeerSamplingInterval - time.Nanosecond)
	roster.ConfirmReachable(t.Context(), yagomodel.Hash("AAAAAAAAAAAA"))
	roster.ConfirmReachable(t.Context(), yagomodel.Hash("BBBBBBBBBBBB"))

	if base.persistedReads != 1 {
		t.Fatalf(
			"persisted peer count reads = %d, want only the boot sample",
			base.persistedReads,
		)
	}
	if observer.lastKnown != 0 {
		t.Fatalf("known gauge = %d, want the boot sample of 0", observer.lastKnown)
	}
	if observer.lastLive != 2 {
		t.Fatalf("reachable gauge = %d, want 2 after both calls", observer.lastLive)
	}
}

func TestObservedPeerRosterRereadsPersistedPeerCountAfterSamplingInterval(t *testing.T) {
	clock := newPeerRosterClock()
	base := &persistedPeerCountRoster{}
	observer := &recordingPeerMetrics{}
	roster := observePeerRosterWithClock(t.Context(), base, observer, clock.Now)

	base.known = 7
	clock.advance(knownPeerSamplingInterval)
	roster.ConfirmReachable(t.Context(), yagomodel.Hash("AAAAAAAAAAAA"))

	if base.persistedReads != 2 {
		t.Fatalf(
			"persisted peer count reads = %d, want a fresh sample past the interval",
			base.persistedReads,
		)
	}
	if observer.lastKnown != 7 {
		t.Fatalf("known gauge = %d, want the resampled 7", observer.lastKnown)
	}
}

func TestObservedPeerRosterKeepsKnownGaugeWhenPersistedCountFails(t *testing.T) {
	clock := newPeerRosterClock()
	base := &persistedPeerCountRoster{}
	base.known = 5
	observer := &recordingPeerMetrics{}
	roster := observePeerRosterWithClock(t.Context(), base, observer, clock.Now)

	base.err = errors.New("count timed out")
	clock.advance(knownPeerSamplingInterval)
	roster.ConfirmReachable(t.Context(), yagomodel.Hash("AAAAAAAAAAAA"))

	if observer.lastKnown != 5 {
		t.Fatalf("known gauge = %d, want the last good sample of 5", observer.lastKnown)
	}
	if observer.lastLive != 1 {
		t.Fatalf("reachable gauge = %d, want 1 despite the failed count", observer.lastLive)
	}
}

func TestObservedPeerRosterForwardsPeerObservations(t *testing.T) {
	when := time.Unix(100, 0)
	base := &observationCountingRoster{observation: peerroster.PeerObservation{
		Seed: yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")}, LastSeen: when,
	}}
	wrapped := observePeerRoster(t.Context(), base, &recordingPeerMetrics{})
	reader := wrapped.(peerroster.ObservationReader)
	observations, known, reachable, err := reader.PeerObservations(t.Context())
	if err != nil || known != 1 || reachable != 0 || len(observations) != 1 ||
		observations[0].LastSeen != when {
		t.Fatalf("PeerObservations = %+v/%d/%d/%v", observations, known, reachable, err)
	}
	observation, found, err := reader.PeerObservation(
		t.Context(), yagomodel.Hash("AAAAAAAAAAAA"),
	)
	if err != nil || !found || observation.LastSeen != when {
		t.Fatalf("PeerObservation = %+v/%v/%v", observation, found, err)
	}
	countReader := wrapped.(observedKnownPeerCounter)
	if count, err := countReader.ObservedKnownPeerCount(t.Context()); err != nil || count != 0 {
		t.Fatalf("ObservedKnownPeerCount = %d/%v", count, err)
	}

	base.err = errors.New("read failed")
	if _, _, _, err := reader.PeerObservations(t.Context()); err == nil {
		t.Fatal("PeerObservations should surface the wrapped failure")
	}
	if _, _, err := reader.PeerObservation(
		t.Context(), yagomodel.Hash("AAAAAAAAAAAA"),
	); err == nil {
		t.Fatal("PeerObservation should surface the wrapped failure")
	}
	if _, err := countReader.ObservedKnownPeerCount(t.Context()); err == nil {
		t.Fatal("ObservedKnownPeerCount should surface the wrapped failure")
	}
}

func TestObservedPeerRosterReportsUnsupportedObservations(t *testing.T) {
	wrapped := observePeerRoster(
		t.Context(), &countingPeerRoster{}, &recordingPeerMetrics{},
	)
	reader := wrapped.(peerroster.ObservationReader)
	if _, _, _, err := reader.PeerObservations(
		t.Context(),
	); !errors.Is(err, errPeerObservationsUnavailable) {
		t.Fatalf("PeerObservations error = %v", err)
	}
	if _, _, err := reader.PeerObservation(
		t.Context(), yagomodel.Hash("AAAAAAAAAAAA"),
	); !errors.Is(err, errPeerObservationsUnavailable) {
		t.Fatalf("PeerObservation error = %v", err)
	}
	if count, err := wrapped.(observedKnownPeerCounter).ObservedKnownPeerCount(
		t.Context(),
	); err != nil || count != 0 {
		t.Fatalf("fallback ObservedKnownPeerCount = %d/%v", count, err)
	}
}
