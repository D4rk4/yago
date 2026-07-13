package yagonode

import (
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func peerSearchFailureTotal(failures []searchcore.PartialFailure) int {
	total := 0
	for _, failure := range failures {
		switch failure.Source {
		case searchcore.PartialFailureSourceRemoteYaCy,
			searchcore.PartialFailureSourcePeerReputation:
			total++

			continue
		case searchcore.PartialFailureSourceExactStage,
			searchcore.PartialFailureSourceLocalExactStage,
			searchcore.PartialFailureSourceFuzzyStage,
			searchcore.PartialFailureSourceLocalSearch,
			searchcore.PartialFailureSourceWeb:
			continue
		}
		if _, err := yagomodel.ParseHash(failure.Source); err == nil {
			total++
		}
	}

	return total
}
