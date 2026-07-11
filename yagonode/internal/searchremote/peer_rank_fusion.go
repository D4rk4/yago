package searchremote

import (
	"cmp"
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type peerSearchCompletion struct {
	requestPosition int
	result          peerSearchResult
}

func orderedPeerSearchResults(results []peerSearchResult) []peerSearchResult {
	ordered := slices.Clone(results)
	slices.SortStableFunc(ordered, func(a, b peerSearchResult) int {
		return cmp.Or(
			strings.Compare(a.peer.Hash.String(), b.peer.Hash.String()),
			strings.Compare(a.term.String(), b.term.String()),
		)
	})

	return ordered
}

func fusePeerRankings(peerRankings [][]searchcore.Result) []searchcore.Result {
	fused := searchcore.FuseByReciprocalRank(peerRankings...)
	for index := range fused {
		fused[index].Evidence = fused[index].Evidence.With(
			searchcore.SignalPeerReputation,
			1,
		)
	}

	return sortedFusedPeerResults(fused)
}

func fuseWeightedPeerRankings(
	peerRankings [][]searchcore.Result,
	peerInfluenceWeights []float64,
	peerReputationWeights []float64,
) []searchcore.Result {
	if len(peerRankings) != len(peerInfluenceWeights) ||
		len(peerRankings) != len(peerReputationWeights) {
		return fusePeerRankings(peerRankings)
	}
	fused := searchcore.FuseByReciprocalRank(peerRankings...)
	weightedScores := make(map[string]float64, len(fused))
	weightedSupport := make(map[string]float64, len(fused))
	reputationNumerators := make(map[string]float64, len(fused))
	reputationDenominators := make(map[string]float64, len(fused))
	influenceByRanking := make(map[int]float64, len(peerInfluenceWeights))
	reputationByRanking := make(map[int]float64, len(peerReputationWeights))
	for index, weight := range peerInfluenceWeights {
		influenceByRanking[index] = weight
	}
	for index, weight := range peerReputationWeights {
		reputationByRanking[index] = weight
	}
	for index, ranking := range peerRankings {
		for _, contribution := range searchcore.FuseByReciprocalRank(ranking) {
			identity := remoteResultIdentity(contribution)
			weightedScores[identity] += contribution.Score * influenceByRanking[index]
			weightedSupport[identity] += influenceByRanking[index]
			reputationNumerators[identity] += contribution.Score * reputationByRanking[index]
			reputationDenominators[identity] += contribution.Score
		}
	}
	for index := range fused {
		identity := remoteResultIdentity(fused[index])
		fused[index].Score = weightedScores[identity]
		fused[index].Evidence = fused[index].Evidence.With(
			searchcore.SignalPeerSupport,
			weightedSupport[identity],
		)
		fused[index].Evidence = fused[index].Evidence.With(
			searchcore.SignalPeerReputation,
			reputationNumerators[identity]/reputationDenominators[identity],
		)
	}

	return sortedFusedPeerResults(fused)
}

func sortedFusedPeerResults(fused []searchcore.Result) []searchcore.Result {
	slices.SortStableFunc(fused, func(a, b searchcore.Result) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			strings.Compare(remoteResultIdentity(a), remoteResultIdentity(b)),
		)
	})

	return fused
}

func peerRankingIdentity(peer yagomodel.Seed) string {
	if peer.Hash != "" {
		return "hash:" + peer.Hash.String()
	}
	if address, ok := peer.NetworkAddress(); ok {
		return "address:" + address
	}

	return "seed:" + peer.String()
}
