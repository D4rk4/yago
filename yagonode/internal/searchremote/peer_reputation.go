package searchremote

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
)

const DefaultMaximumNetworkGroupInfluence = 1.5

type ReputationSnapshotSource interface {
	Snapshot(context.Context, time.Time) (peerreputation.Snapshot, error)
}

type ReputationObservationSink interface {
	Observe(context.Context, []peerreputation.Observation)
}

type ReputationNetworkGroup func(yagomodel.Seed) peerreputation.NetworkGroupKey

type reputationSession struct {
	snapshot              peerreputation.Snapshot
	snapshotAvailable     bool
	observations          ReputationObservationSink
	networkGroup          ReputationNetworkGroup
	maximumGroupInfluence float64
	observedAt            time.Time
	pending               []peerreputation.Observation
}

func (s searcher) beginReputation(
	ctx context.Context,
) (*reputationSession, error) {
	if s.reputationSnapshots == nil && s.reputationObservations == nil {
		return nil, nil
	}
	session := &reputationSession{
		observations:          s.reputationObservations,
		networkGroup:          s.reputationNetworkGroup,
		maximumGroupInfluence: s.maximumNetworkGroupInfluence,
		observedAt:            s.reputationClock(),
	}
	if s.reputationSnapshots == nil {
		return session, nil
	}
	snapshot, err := s.reputationSnapshots.Snapshot(ctx, session.observedAt)
	if err != nil {
		return session, fmt.Errorf("peer reputation snapshot: %w", err)
	}
	session.snapshot = snapshot
	session.snapshotAvailable = true

	return session, nil
}

func (session *reputationSession) record(
	peer yagomodel.Seed,
	outcome peerreputation.Outcome,
) {
	if session == nil || session.observations == nil || peer.Hash == "" {
		return
	}
	identity := peerreputation.SignedPeerIdentity(peer.Hash.String())
	session.pending = append(session.pending, peerreputation.Observation{
		Peer:         identity,
		NetworkGroup: session.group(peer, identity),
		Outcome:      outcome,
		ObservedAt:   session.observedAt,
	})
}

func (session *reputationSession) group(
	peer yagomodel.Seed,
	identity peerreputation.SignedPeerIdentity,
) peerreputation.NetworkGroupKey {
	if session.networkGroup != nil {
		if group := session.networkGroup(peer); group != "" {
			return group
		}
	}

	return peerreputation.NetworkGroupKey("peer:" + string(identity))
}

func (session *reputationSession) flush(ctx context.Context) {
	if session == nil || session.observations == nil || len(session.pending) == 0 {
		return
	}
	ordered := slices.Clone(session.pending)
	slices.SortFunc(ordered, func(left, right peerreputation.Observation) int {
		return cmp.Or(
			strings.Compare(string(left.Peer), string(right.Peer)),
			strings.Compare(string(left.NetworkGroup), string(right.NetworkGroup)),
			cmp.Compare(left.Outcome, right.Outcome),
		)
	})
	session.observations.Observe(context.WithoutCancel(ctx), ordered)
	session.pending = nil
}

func (session *reputationSession) fusionWeights(
	peerOrder []string,
	peers map[string]yagomodel.Seed,
) (map[string]float64, map[string]float64, error) {
	influenceWeights := make(map[string]float64, len(peerOrder))
	reputationWeights := make(map[string]float64, len(peerOrder))
	for _, identity := range peerOrder {
		influenceWeights[identity] = 1
		reputationWeights[identity] = 1
	}
	if session == nil || !session.snapshotAvailable {
		return influenceWeights, reputationWeights, nil
	}
	influences := make([]peerreputation.PeerInfluence, 0, len(peerOrder))
	identities := make(map[peerreputation.SignedPeerIdentity]string, len(peerOrder))
	for _, rankingIdentity := range peerOrder {
		peer := peers[rankingIdentity]
		if peer.Hash == "" {
			continue
		}
		identity := peerreputation.SignedPeerIdentity(peer.Hash.String())
		identities[identity] = rankingIdentity
		influences = append(influences, peerreputation.PeerInfluence{
			Peer:         identity,
			NetworkGroup: session.group(peer, identity),
			BaseWeight:   1,
		})
	}
	capped, err := session.snapshot.CapNetworkGroupInfluence(
		influences,
		session.maximumGroupInfluence,
	)
	if err != nil {
		return influenceWeights, reputationWeights, fmt.Errorf(
			"cap peer network-group influence: %w",
			err,
		)
	}
	for _, influence := range capped {
		rankingIdentity := identities[influence.Peer]
		influenceWeights[rankingIdentity] = influence.Weight
		reputationWeights[rankingIdentity] = influence.ReputationWeight
	}

	return influenceWeights, reputationWeights, nil
}

func observationOutcome(err error, invalid bool) peerreputation.Outcome {
	if invalid || errors.Is(err, errRemoteSearchInvalidResult) {
		return peerreputation.OutcomeInvalidResult
	}
	if err == nil {
		return peerreputation.OutcomeSuccess
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return peerreputation.OutcomeTimeout
	}

	return peerreputation.OutcomeFailure
}

func reputationClockOrDefault(clock func() time.Time) func() time.Time {
	if clock == nil {
		return time.Now
	}

	return clock
}

func maximumGroupInfluenceOrDefault(value float64) float64 {
	if value > 0 {
		return value
	}

	return DefaultMaximumNetworkGroupInfluence
}
