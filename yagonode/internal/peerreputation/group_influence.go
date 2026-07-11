package peerreputation

import (
	"fmt"
	"sort"
)

type PeerInfluence struct {
	Peer         SignedPeerIdentity
	NetworkGroup NetworkGroupKey
	BaseWeight   float64
}

type CappedPeerInfluence struct {
	Peer             SignedPeerIdentity
	NetworkGroup     NetworkGroupKey
	ReputationWeight float64
	Weight           float64
}

func (snapshot Snapshot) CapNetworkGroupInfluence(
	influences []PeerInfluence,
	maximumGroupWeight float64,
) ([]CappedPeerInfluence, error) {
	if !finitePositive(maximumGroupWeight) || maximumGroupWeight > maximumInfluenceWeight {
		return nil, fmt.Errorf("peer reputation group influence cap is invalid")
	}
	ordered := append([]PeerInfluence(nil), influences...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].Peer < ordered[right].Peer
	})
	result := make([]CappedPeerInfluence, len(ordered))
	groupTotals := map[NetworkGroupKey]float64{}
	for index, influence := range ordered {
		if err := validateBoundedLabel(
			string(influence.Peer),
			"signed peer identity",
		); err != nil {
			return nil, err
		}
		if err := validateBoundedLabel(
			string(influence.NetworkGroup),
			"network group",
		); err != nil {
			return nil, err
		}
		if !finite(influence.BaseWeight) || influence.BaseWeight < 0 ||
			influence.BaseWeight > maximumInfluenceWeight {
			return nil, fmt.Errorf("peer reputation base influence is invalid")
		}
		if index > 0 && ordered[index-1].Peer == influence.Peer {
			return nil, fmt.Errorf("peer reputation influence identity is duplicated")
		}
		reputationWeight := snapshot.Peer(influence.Peer).FusionWeight
		weight := influence.BaseWeight * reputationWeight
		result[index] = CappedPeerInfluence{
			Peer:             influence.Peer,
			NetworkGroup:     influence.NetworkGroup,
			ReputationWeight: reputationWeight,
			Weight:           weight,
		}
		groupTotals[influence.NetworkGroup] += weight
	}
	groupUsed := map[NetworkGroupKey]float64{}
	for index := range result {
		total := groupTotals[result[index].NetworkGroup]
		if total <= maximumGroupWeight {
			continue
		}
		remaining := max(maximumGroupWeight-groupUsed[result[index].NetworkGroup], 0)
		scaled := result[index].Weight * maximumGroupWeight / total
		result[index].Weight = min(scaled, remaining)
		groupUsed[result[index].NetworkGroup] += result[index].Weight
	}

	return result, nil
}
