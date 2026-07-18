package searchindex

import (
	"strings"

	"github.com/blevesearch/bleve/v2/search"
)

type storedAnalyzerSequenceWitness struct {
	current        *search.Location
	position       uint64
	start          uint64
	end            uint64
	arrayPositions search.ArrayPositions
}

func collapseStoredCJKRequirements(
	matcher *storedEvidenceMatcher,
	values []string,
	field *storedFieldEvidence,
) {
	requirements := make(search.TermLocationMap, len(matcher.rawRequirements))
	exact := make(search.TermLocationMap, len(matcher.rawRequirements))
	for rawRequirement, raw := range matcher.rawRequirements {
		locations := storedAnalyzerRequirementLocations(
			matcher,
			field.targetTerms,
			rawRequirement,
		)
		requirements[raw] = locations
		for _, location := range locations {
			value, found := storedLocationValue(values, location)
			if !found || location.Start >= location.End || location.End > uint64(len(value)) {
				continue
			}
			if strings.EqualFold(value[location.Start:location.End], raw) {
				exact[raw] = appendStoredLocation(exact[raw], location)
			}
		}
	}
	field.requirementTerms = requirements
	field.exactTerms = exact
}

func storedAnalyzerRequirementLocations(
	matcher *storedEvidenceMatcher,
	locations map[int]search.Locations,
	rawRequirement int,
) search.Locations {
	groups := storedAnalyzerTargetGroups(matcher.targets, rawRequirement)
	if len(groups) == 0 {
		return nil
	}
	first := storedAnalyzerGroupLocations(locations, groups[0])
	frontier := make([]storedAnalyzerSequenceWitness, 0, len(first))
	for _, location := range first {
		frontier = append(frontier, storedAnalyzerSequenceWitness{
			current:        location,
			position:       location.Pos,
			start:          location.Start,
			end:            location.End,
			arrayPositions: append(search.ArrayPositions(nil), location.ArrayPositions...),
		})
	}
	for groupIndex := 1; groupIndex < len(groups); groupIndex++ {
		candidates := storedAnalyzerGroupLocations(locations, groups[groupIndex])
		next := make([]storedAnalyzerSequenceWitness, 0, len(candidates))
		gap := groups[groupIndex].position - groups[groupIndex-1].position
		for _, candidate := range candidates {
			for _, previous := range frontier {
				if !storedAnalyzerLocationsAdjacent(
					previous.current,
					candidate,
					gap,
					true,
				) {
					continue
				}
				next = append(next, storedAnalyzerSequenceWitness{
					current:        candidate,
					position:       previous.position,
					start:          min(previous.start, candidate.Start),
					end:            max(previous.end, candidate.End),
					arrayPositions: previous.arrayPositions,
				})

				break
			}
		}
		if len(next) == 0 {
			return nil
		}
		frontier = next
	}
	out := make(search.Locations, 0, min(len(frontier), maximumTermPositionsPerField))
	for _, witness := range frontier {
		location := &search.Location{
			Pos:            witness.position,
			Start:          witness.start,
			End:            witness.end,
			ArrayPositions: append(search.ArrayPositions(nil), witness.arrayPositions...),
		}
		if len(out) == 0 || !sameStoredLocationSpan(out[len(out)-1], location) {
			out = append(out, location)
		}
		if len(out) == maximumTermPositionsPerField {
			break
		}
	}

	return out
}

func storedLocationValue(
	values []string,
	location *search.Location,
) (string, bool) {
	index := uint64(0)
	if len(location.ArrayPositions) > 0 {
		index = location.ArrayPositions[0]
	}
	if index >= uint64(len(values)) {
		return "", false
	}

	return values[index], true
}

func sameStoredLocationSpan(left *search.Location, right *search.Location) bool {
	return sameStoredLocation(left, right) && left.Start == right.Start && left.End == right.End
}
