package searchindex

import (
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2/search"
)

func TestPublishedRequirementLocationsPreservesRawIdentities(t *testing.T) {
	matcher := newStoredEvidenceMatcher(
		SearchRequest{Terms: []string{" Gaming ", "games", "missing"}},
		"en",
	)
	locations := publishedRequirementLocations(
		matcher.rawRequirementAnalyzedTerms,
		search.TermLocationMap{"game": {{Pos: 3}, {Pos: 8}}},
	)
	total := 0
	for _, identity := range []string{"gaming", "games"} {
		positions := boundedFieldTermPositions(
			SearchRequest{Terms: []string{identity}},
			search.TermLocationMap{identity: locations[identity]},
		)[identity]
		if len(positions) != 1 || !slices.Contains([]int{3, 8}, positions[0]) {
			t.Fatalf("%s positions = %v", identity, positions)
		}
		total += len(positions)
	}
	if total != 2 {
		t.Fatalf("published positions = %#v", locations)
	}
	if _, exposed := locations["game"]; exposed {
		t.Fatalf("analyzed identity was published: %#v", locations)
	}
	if positions, published := locations["missing"]; !published || len(positions) != 0 {
		t.Fatalf("retained unmatched identity = %#v", locations)
	}
}

func TestPublishedRequirementLocationsAssignsOneOccurrenceOnce(t *testing.T) {
	matcher := newStoredEvidenceMatcher(
		SearchRequest{Terms: []string{"gaming", "games"}},
		"en",
	)
	locations := publishedRequirementLocations(
		matcher.rawRequirementAnalyzedTerms,
		search.TermLocationMap{"game": {{Pos: 3}}},
	)
	if _, found := locations["gaming"]; !found {
		t.Fatalf("gaming requirement missing: %#v", locations)
	}
	if _, found := locations["games"]; !found {
		t.Fatalf("games requirement missing: %#v", locations)
	}
	if len(locations["gaming"])+len(locations["games"]) != 1 {
		t.Fatalf("one occurrence published more than once: %#v", locations)
	}
}

func TestPublishedRequirementLocationsDoesNotReuseSynonymCoordinates(t *testing.T) {
	location := &search.Location{Pos: 4}
	locations := publishedRequirementLocations(
		map[string][]string{
			"alpha": {"first"},
			"beta":  {"second"},
		},
		search.TermLocationMap{
			"first":  {location},
			"second": {location},
		},
	)
	if len(locations["alpha"])+len(locations["beta"]) != 1 {
		t.Fatalf("shared coordinate published twice: %#v", locations)
	}
}

func TestPublishedRequirementLocationsFallsBackWithoutRequirements(t *testing.T) {
	locations := search.TermLocationMap{"alpha": {{Pos: 1}}}
	if got := publishedRequirementLocations(nil, locations); len(got["alpha"]) != 1 {
		t.Fatalf("fallback locations = %#v", got)
	}
}
