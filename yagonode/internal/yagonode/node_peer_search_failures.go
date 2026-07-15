package yagonode

import (
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func peerSearchFailureTotal(failures []searchcore.PartialFailure) int {
	peers := map[yagomodel.Hash]struct{}{}
	for _, failure := range failures {
		switch failure.Source {
		case searchcore.PartialFailureSourceRemoteYaCy,
			searchcore.PartialFailureSourceRemoteStage,
			searchcore.PartialFailureSourcePeerReputation,
			searchcore.PartialFailureSourceExactStage,
			searchcore.PartialFailureSourceLocalExactStage,
			searchcore.PartialFailureSourceFuzzyStage,
			searchcore.PartialFailureSourceLocalSearch,
			searchcore.PartialFailureSourceWeb:
			continue
		}
		peer, err := yagomodel.ParseHash(failure.Source)
		if err == nil {
			peers[peer] = struct{}{}
		}
	}

	return len(peers)
}

func federationSearchUnavailable(failures []searchcore.PartialFailure) bool {
	for _, failure := range failures {
		switch failure.Source {
		case searchcore.PartialFailureSourceRemoteYaCy,
			searchcore.PartialFailureSourceRemoteStage,
			searchcore.PartialFailureSourcePeerReputation:
			return true
		}
	}

	return false
}
