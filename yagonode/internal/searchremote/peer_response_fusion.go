package searchremote

import (
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func fusedPeerResponseResults(
	reputation *reputationSession,
	peerOrder []string,
	peerResults map[string][]searchcore.Result,
	peerSeeds map[string]yagomodel.Seed,
) ([]searchcore.Result, error) {
	peerRankings := make([][]searchcore.Result, 0, len(peerOrder))
	peerInfluenceWeights := make([]float64, 0, len(peerOrder))
	peerReputationWeights := make([]float64, 0, len(peerOrder))
	influenceWeights, reputationWeights, reputationErr := reputation.fusionWeights(
		peerOrder,
		peerSeeds,
	)
	for _, identity := range peerOrder {
		peerRankings = append(peerRankings, dedupeSearchResults(peerResults[identity]))
		peerInfluenceWeights = append(peerInfluenceWeights, influenceWeights[identity])
		peerReputationWeights = append(peerReputationWeights, reputationWeights[identity])
	}
	fused := fusePeerRankings(peerRankings)
	if reputation != nil && reputation.snapshotAvailable && reputationErr == nil {
		fused = fuseWeightedPeerRankings(
			peerRankings,
			peerInfluenceWeights,
			peerReputationWeights,
		)
	}

	return fused, reputationErr
}
