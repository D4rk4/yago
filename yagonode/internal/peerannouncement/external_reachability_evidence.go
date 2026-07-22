package peerannouncement

import (
	"context"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	externalReachabilityLifetime      = 15 * time.Minute
	externalReachabilityObserverLimit = 1024
)

type ExternalReachabilityEvidence struct {
	mu            sync.Mutex
	lifetime      time.Duration
	observerLimit int
	now           func() time.Time
	observations  map[yagomodel.Hash]externalReachabilityObservation
	published     yagomodel.PeerType
}

type externalReachabilityObservation struct {
	classification yagomodel.PeerType
	observedAt     time.Time
}

type ExternalReachabilitySnapshot struct {
	PeerType   yagomodel.PeerType
	ObservedAt time.Time
	Known      bool
}

func NewExternalReachabilityEvidence() *ExternalReachabilityEvidence {
	return newExternalReachabilityEvidence(
		externalReachabilityObserverLimit,
		time.Now,
	)
}

func newExternalReachabilityEvidence(
	observerLimit int,
	now func() time.Time,
) *ExternalReachabilityEvidence {
	return &ExternalReachabilityEvidence{
		lifetime:      externalReachabilityLifetime,
		observerLimit: observerLimit,
		now:           now,
		observations:  make(map[yagomodel.Hash]externalReachabilityObservation, observerLimit),
		published:     yagomodel.PeerVirgin,
	}
}

func (e *ExternalReachabilityEvidence) Observe(
	observer yagomodel.Hash,
	classification yagomodel.PeerType,
) {
	e.mu.Lock()
	defer e.mu.Unlock()

	observedAt := e.now()
	e.removeExpired(observedAt)
	if classification != yagomodel.PeerSenior && classification != yagomodel.PeerPrincipal {
		classification = yagomodel.PeerJunior
	}
	if _, known := e.observations[observer]; !known && len(e.observations) >= e.observerLimit {
		e.removeOldest()
	}
	e.observations[observer] = externalReachabilityObservation{
		classification: classification,
		observedAt:     observedAt,
	}
	e.publishCurrentClassification()
}

func (e *ExternalReachabilityEvidence) Reachable(ctx context.Context) bool {
	snapshot := e.Snapshot(ctx)

	return snapshot.Known && snapshot.PeerType == yagomodel.PeerSenior
}

func (e *ExternalReachabilityEvidence) Snapshot(context.Context) ExternalReachabilitySnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.removeExpired(e.now())

	return e.publishCurrentClassification()
}

func (e *ExternalReachabilityEvidence) currentSnapshot() ExternalReachabilitySnapshot {
	snapshot := ExternalReachabilitySnapshot{PeerType: yagomodel.PeerJunior}
	for _, observation := range e.observations {
		positive := observation.classification == yagomodel.PeerSenior ||
			observation.classification == yagomodel.PeerPrincipal
		if positive && snapshot.PeerType != yagomodel.PeerSenior {
			snapshot.PeerType = yagomodel.PeerSenior
			snapshot.ObservedAt = observation.observedAt
		} else if positive == (snapshot.PeerType == yagomodel.PeerSenior) &&
			observation.observedAt.After(snapshot.ObservedAt) {
			snapshot.ObservedAt = observation.observedAt
		}
	}
	snapshot.Known = len(e.observations) != 0

	return snapshot
}

func (e *ExternalReachabilityEvidence) PublishedPeerType(context.Context) yagomodel.PeerType {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removeExpired(e.now())
	e.publishCurrentClassification()

	return e.published
}

func (e *ExternalReachabilityEvidence) publishCurrentClassification() ExternalReachabilitySnapshot {
	snapshot := e.currentSnapshot()
	if snapshot.Known {
		e.published = snapshot.PeerType
	}

	return snapshot
}

func (e *ExternalReachabilityEvidence) PeerType(ctx context.Context) (yagomodel.PeerType, bool) {
	snapshot := e.Snapshot(ctx)

	return snapshot.PeerType, snapshot.Known
}

func (e *ExternalReachabilityEvidence) removeExpired(now time.Time) {
	for observer, observation := range e.observations {
		if !observation.observedAt.Add(e.lifetime).After(now) {
			delete(e.observations, observer)
		}
	}
}

func (e *ExternalReachabilityEvidence) removeOldest() {
	var oldestObserver yagomodel.Hash
	var oldestObservation time.Time
	for observer, observation := range e.observations {
		if oldestObserver == "" || observation.observedAt.Before(oldestObservation) ||
			(observation.observedAt.Equal(oldestObservation) && observer.String() < oldestObserver.String()) {
			oldestObserver = observer
			oldestObservation = observation.observedAt
		}
	}
	delete(e.observations, oldestObserver)
}
