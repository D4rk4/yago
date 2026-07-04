package dhttarget

import (
	"fmt"
	"slices"
)

func candidateRedundancy(config Config) int {
	if config.CandidateRedundancy > config.Redundancy {
		return config.CandidateRedundancy
	}

	return config.Redundancy
}

func chooseTargets(
	targets []Target,
	redundancy int,
	randomTargetIndex func(int) (int, error),
) ([]Target, error) {
	if len(targets) <= redundancy {
		return targets, nil
	}
	if randomTargetIndex == nil {
		return targets[:redundancy], nil
	}

	candidates := slices.Clone(targets)
	selected := make([]Target, 0, redundancy)
	for range redundancy {
		index, err := randomTargetIndex(len(candidates))
		if err != nil {
			return nil, fmt.Errorf("choose dht target: %w", err)
		}
		if index < 0 || index >= len(candidates) {
			return nil, fmt.Errorf(
				"choose dht target: index %d outside %d candidates",
				index,
				len(candidates),
			)
		}
		selected = append(selected, candidates[index])
		candidates = slices.Delete(candidates, index, index+1)
	}

	return selected, nil
}
