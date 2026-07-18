package searchindex

import (
	"strings"

	"github.com/blevesearch/bleve/v2/search"
)

func (f *storedFieldEvidence) addMatches(
	matcher *storedEvidenceMatcher,
	matched []int,
	location *search.Location,
	surface string,
) {
	f.addAnalyzedMatches(matcher, matched, location)
	f.addRawMatch(matcher, matched, location, surface)
	f.addExactMatch(matcher, matched, location, surface)
}

func (f *storedFieldEvidence) addAnalyzedMatches(
	matcher *storedEvidenceMatcher,
	matched []int,
	location *search.Location,
) {
	if isCJKAnalyzer(matcher.name) && matcher.name != "cjk" {
		for _, targetIndex := range matched {
			f.targetTerms[targetIndex] = appendStoredLocation(
				f.targetTerms[targetIndex],
				location,
			)
		}
	}
	for matchedIndex, targetIndex := range matched {
		target := matcher.targets[targetIndex]
		if matchedAnalyzedRequirementSeen(
			matcher,
			matched[:matchedIndex],
			target.requirement,
		) {
			continue
		}
		f.terms[target.analyzed] = appendStoredLocation(
			f.terms[target.analyzed],
			location,
		)
		f.latest[target.requirement] = location
	}
}

func (f *storedFieldEvidence) addRawMatch(
	matcher *storedEvidenceMatcher,
	matched []int,
	location *search.Location,
	surface string,
) {
	target, found := f.selectRawRequirement(matcher, matched, surface)
	if !found {
		return
	}
	f.requirementTerms[target.raw] = appendStoredLocation(
		f.requirementTerms[target.raw],
		location,
	)
	f.latestRaw[target.rawRequirement] = location
	if !isCJKAnalyzer(matcher.name) || matcher.name == "cjk" {
		for _, targetIndex := range matched {
			if matcher.targets[targetIndex].rawRequirement == target.rawRequirement {
				f.targetTerms[targetIndex] = appendStoredLocation(
					f.targetTerms[targetIndex],
					location,
				)
			}
		}
	}
}

func (f *storedFieldEvidence) addExactMatch(
	matcher *storedEvidenceMatcher,
	matched []int,
	location *search.Location,
	surface string,
) {
	surface = strings.TrimSpace(surface)
	for _, targetIndex := range matched {
		target := matcher.targets[targetIndex]
		if !strings.EqualFold(surface, target.raw) {
			continue
		}
		exactLocation := f.exactRequirementLocation(matcher, target, location)
		f.exactTerms[target.raw] = appendStoredLocation(
			f.exactTerms[target.raw],
			exactLocation,
		)
		f.exactLatest[target.rawRequirement] = exactLocation

		break
	}
}

func (f *storedFieldEvidence) selectRawRequirement(
	matcher *storedEvidenceMatcher,
	matched []int,
	surface string,
) (storedEvidenceTarget, bool) {
	surface = strings.TrimSpace(surface)
	if target, found := f.unassignedExactRawRequirement(matcher, matched, surface); found {
		return target, true
	}
	if target, found := f.unassignedRawRequirement(matcher, matched); found {
		return target, true
	}
	if target, found := exactRawRequirement(matcher, matched, surface); found {
		return target, true
	}

	return f.leastRecentlyAssignedRawRequirement(matcher, matched)
}

func (f *storedFieldEvidence) unassignedExactRawRequirement(
	matcher *storedEvidenceMatcher,
	matched []int,
	surface string,
) (storedEvidenceTarget, bool) {
	for matchedIndex, targetIndex := range matched {
		target := matcher.targets[targetIndex]
		if matchedRawRequirementSeen(
			matcher,
			matched[:matchedIndex],
			target.rawRequirement,
		) {
			continue
		}
		if _, assigned := f.latestRaw[target.rawRequirement]; !assigned &&
			strings.EqualFold(surface, target.raw) {
			return target, true
		}
	}

	return storedEvidenceTarget{}, false
}

func (f *storedFieldEvidence) unassignedRawRequirement(
	matcher *storedEvidenceMatcher,
	matched []int,
) (storedEvidenceTarget, bool) {
	for matchedIndex, targetIndex := range matched {
		target := matcher.targets[targetIndex]
		if matchedRawRequirementSeen(
			matcher,
			matched[:matchedIndex],
			target.rawRequirement,
		) {
			continue
		}
		if _, assigned := f.latestRaw[target.rawRequirement]; !assigned {
			return target, true
		}
	}

	return storedEvidenceTarget{}, false
}

func exactRawRequirement(
	matcher *storedEvidenceMatcher,
	matched []int,
	surface string,
) (storedEvidenceTarget, bool) {
	for matchedIndex, targetIndex := range matched {
		target := matcher.targets[targetIndex]
		if matchedRawRequirementSeen(
			matcher,
			matched[:matchedIndex],
			target.rawRequirement,
		) {
			continue
		}
		if strings.EqualFold(surface, target.raw) {
			return target, true
		}
	}

	return storedEvidenceTarget{}, false
}

func (f *storedFieldEvidence) leastRecentlyAssignedRawRequirement(
	matcher *storedEvidenceMatcher,
	matched []int,
) (storedEvidenceTarget, bool) {
	selected := storedEvidenceTarget{}
	found := false
	for matchedIndex, targetIndex := range matched {
		target := matcher.targets[targetIndex]
		if matchedRawRequirementSeen(
			matcher,
			matched[:matchedIndex],
			target.rawRequirement,
		) {
			continue
		}
		if !found {
			selected = target
			found = true

			continue
		}
		selectedLocation := f.latestRaw[selected.rawRequirement]
		targetLocation := f.latestRaw[target.rawRequirement]
		if targetLocation.Pos < selectedLocation.Pos ||
			(targetLocation.Pos == selectedLocation.Pos &&
				target.rawRequirement < selected.rawRequirement) {
			selected = target
		}
	}

	return selected, found
}

func matchedAnalyzedRequirementSeen(
	matcher *storedEvidenceMatcher,
	matched []int,
	requirement int,
) bool {
	for _, targetIndex := range matched {
		if matcher.targets[targetIndex].requirement == requirement {
			return true
		}
	}

	return false
}

func matchedRawRequirementSeen(
	matcher *storedEvidenceMatcher,
	matched []int,
	requirement int,
) bool {
	for _, targetIndex := range matched {
		if matcher.targets[targetIndex].rawRequirement == requirement {
			return true
		}
	}

	return false
}

func appendStoredLocation(
	locations search.Locations,
	location *search.Location,
) search.Locations {
	if len(locations) > 0 && sameStoredLocation(locations[len(locations)-1], location) {
		return locations
	}
	if len(locations) < maximumTermPositionsPerField {
		return append(locations, location)
	}
	locations[len(locations)-1] = location

	return locations
}
