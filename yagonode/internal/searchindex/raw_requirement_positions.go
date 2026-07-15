package searchindex

import (
	"slices"

	"github.com/blevesearch/bleve/v2/search"
)

func publishedRequirementLocations(
	rawRequirementAnalyzedTerms map[string][]string,
	locations search.TermLocationMap,
) search.TermLocationMap {
	if len(rawRequirementAnalyzedTerms) == 0 {
		return locations
	}
	published := make(search.TermLocationMap, len(rawRequirementAnalyzedTerms))
	identitiesByAnalyzed := map[string][]string{}
	for identity, analyzedTerms := range rawRequirementAnalyzedTerms {
		published[identity] = nil
		for _, analyzed := range analyzedTerms {
			identitiesByAnalyzed[analyzed] = append(
				identitiesByAnalyzed[analyzed],
				identity,
			)
		}
	}
	analyzedTerms := make([]string, 0, len(identitiesByAnalyzed))
	for analyzed, candidates := range identitiesByAnalyzed {
		slices.Sort(candidates)
		identitiesByAnalyzed[analyzed] = slices.Compact(candidates)
		analyzedTerms = append(analyzedTerms, analyzed)
	}
	slices.Sort(analyzedTerms)
	assigned := search.Locations{}
	for _, analyzed := range analyzedTerms {
		candidates := identitiesByAnalyzed[analyzed]
		for _, location := range locations[analyzed] {
			if publishedLocationAssigned(assigned, location) {
				continue
			}
			identity := leastPublishedRequirement(published, candidates, analyzed)
			published[identity] = appendStoredLocation(published[identity], location)
			assigned = append(assigned, location)
		}
	}

	return published
}

func leastPublishedRequirement(
	published search.TermLocationMap,
	candidates []string,
	analyzed string,
) string {
	for _, identity := range candidates {
		if identity == analyzed && len(published[identity]) == 0 {
			return identity
		}
	}
	selected := candidates[0]
	for _, identity := range candidates[1:] {
		if len(published[identity]) < len(published[selected]) {
			selected = identity
		}
	}

	return selected
}

func publishedLocationAssigned(
	assigned search.Locations,
	location *search.Location,
) bool {
	for _, existing := range assigned {
		if sameStoredLocation(existing, location) {
			return true
		}
	}

	return false
}
