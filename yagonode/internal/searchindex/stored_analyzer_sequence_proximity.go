package searchindex

import "github.com/blevesearch/bleve/v2/search"

type storedAnalyzerTargetGroup struct {
	targets  []int
	position int
}

func storedSingleRequirementAnalyzerProximity(
	matcher *storedEvidenceMatcher,
	targets map[int]search.Locations,
	allowAnalyzerSequence bool,
) (float64, float64) {
	if !allowAnalyzerSequence || len(matcher.rawRequirements) != 1 ||
		len(matcher.targets) < 2 {
		return 0, 0
	}
	groups := storedAnalyzerTargetGroups(matcher.targets, 0)
	if len(groups) < 2 {
		return 0, 0
	}
	unordered := 0.0
	ordered := 0.0
	if storedAnalyzerSequencePresent(targets, groups, false) {
		unordered = analyzerVariantPairConfidence
	}
	if storedAnalyzerSequencePresent(targets, groups, true) {
		ordered = analyzerVariantPairConfidence
	}

	return unordered, ordered
}

func storedAnalyzerTargetGroups(
	targets []storedEvidenceTarget,
	rawRequirement int,
) []storedAnalyzerTargetGroup {
	groups := make([]storedAnalyzerTargetGroup, 0, len(targets))
	for index, target := range targets {
		if target.rawRequirement != rawRequirement {
			continue
		}
		position := target.analyzerPosition
		if position <= 0 {
			position = index + 1
		}
		if len(groups) > 0 && groups[len(groups)-1].position == position {
			groups[len(groups)-1].targets = append(
				groups[len(groups)-1].targets,
				index,
			)

			continue
		}
		groups = append(groups, storedAnalyzerTargetGroup{
			targets:  []int{index},
			position: position,
		})
		if len(groups) == maximumTermPositionsPerField {
			break
		}
	}

	return groups
}

func storedAnalyzerSequencePresent(
	locations map[int]search.Locations,
	groups []storedAnalyzerTargetGroup,
	ordered bool,
) bool {
	frontier := storedAnalyzerGroupLocations(locations, groups[0])
	for index := 1; index < len(groups); index++ {
		candidates := storedAnalyzerGroupLocations(locations, groups[index])
		next := make(search.Locations, 0, len(candidates))
		for _, candidate := range candidates {
			for _, previous := range frontier {
				if storedAnalyzerLocationsAdjacent(
					previous,
					candidate,
					groups[index].position-groups[index-1].position,
					ordered,
				) {
					next = append(next, candidate)

					break
				}
			}
		}
		if len(next) == 0 {
			return false
		}
		frontier = next
	}

	return len(frontier) > 0
}

func storedAnalyzerGroupLocations(
	locations map[int]search.Locations,
	group storedAnalyzerTargetGroup,
) search.Locations {
	out := make(search.Locations, 0, len(group.targets))
	for _, targetIndex := range group.targets {
		for _, candidate := range locations[targetIndex] {
			duplicate := false
			for _, existing := range out {
				if sameStoredLocation(existing, candidate) {
					duplicate = true

					break
				}
			}
			if !duplicate {
				out = append(out, candidate)
			}
		}
	}

	return out
}

func storedAnalyzerLocationsAdjacent(
	left *search.Location,
	right *search.Location,
	expectedGap int,
	ordered bool,
) bool {
	if left == nil || right == nil ||
		!left.ArrayPositions.Equals(right.ArrayPositions) {
		return false
	}
	if ordered {
		return right.Pos >= left.Pos &&
			right.Pos-left.Pos == storedLocationCoordinate(expectedGap)
	}
	if left.Pos > right.Pos {
		return left.Pos-right.Pos <= storedLocationCoordinate(sdmUnorderedWindow)
	}

	return right.Pos-left.Pos <= storedLocationCoordinate(sdmUnorderedWindow)
}
