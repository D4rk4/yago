package peerreputation

import (
	"math"
	"time"
)

type PeerReputation struct {
	Peer                 SignedPeerIdentity `json:"signed_peer_identity"`
	NetworkGroup         NetworkGroupKey    `json:"network_group"`
	Known                bool               `json:"known"`
	Reliability          float64            `json:"reliability"`
	FusionWeight         float64            `json:"fusion_weight"`
	Confidence           float64            `json:"confidence"`
	SuccessEvidence      float64            `json:"success_evidence"`
	FailureEvidence      float64            `json:"failure_evidence"`
	LastObservedUnixNano int64              `json:"last_observed_unix_nano"`
}

func reputationAt(record peerRecord, atNanos int64, configuration Configuration) PeerReputation {
	effectiveAt := max(atNanos, record.LastObservedUnixNano)
	factor := decayFactor(record.LastObservedUnixNano, effectiveAt, configuration.HalfLife)
	success := record.SuccessEvidence * factor
	failure := record.FailureEvidence * factor
	evidence := success + failure
	prior := configuration.PriorSuccess + configuration.PriorFailure
	neutral := configuration.PriorSuccess / prior
	posterior := (configuration.PriorSuccess + success) / (prior + evidence)
	confidence := evidence / (prior + evidence)
	reliability := neutral + confidence*(posterior-neutral)
	fusionWeight := clamp(reliability/neutral, minimumFusionWeight, maximumFusionWeight)

	return PeerReputation{
		Peer:                 record.Peer,
		NetworkGroup:         record.NetworkGroup,
		Known:                true,
		Reliability:          clamp(reliability, 0, 1),
		FusionWeight:         fusionWeight,
		Confidence:           clamp(confidence, 0, 1),
		SuccessEvidence:      success,
		FailureEvidence:      failure,
		LastObservedUnixNano: record.LastObservedUnixNano,
	}
}

func decayFactor(fromNanos, toNanos int64, halfLife time.Duration) float64 {
	if toNanos <= fromNanos {
		return 1
	}

	return math.Exp2(-float64(toNanos-fromNanos) / float64(halfLife))
}

func addObservation(
	record peerRecord,
	observation normalizedObservation,
	configuration Configuration,
) peerRecord {
	effectiveAt := max(record.LastObservedUnixNano, observation.observedAtNanos)
	factor := decayFactor(record.LastObservedUnixNano, effectiveAt, configuration.HalfLife)
	record.SuccessEvidence *= factor
	record.FailureEvidence *= factor
	success, failure, _ := outcomeEvidence(observation.outcome)
	record.SuccessEvidence += success
	record.FailureEvidence += failure
	record.SuccessEvidence, record.FailureEvidence = boundedEvidence(
		record.SuccessEvidence,
		record.FailureEvidence,
	)
	if observation.observedAtNanos >= record.LastObservedUnixNano {
		record.NetworkGroup = observation.networkGroup
	}
	record.LastObservedUnixNano = effectiveAt

	return record
}

func boundedEvidence(success, failure float64) (float64, float64) {
	total := success + failure
	if total <= maximumEvidence {
		return success, failure
	}
	scale := maximumEvidence / total

	return success * scale, failure * scale
}

func clamp(value, minimum, maximum float64) float64 {
	return min(max(value, minimum), maximum)
}
