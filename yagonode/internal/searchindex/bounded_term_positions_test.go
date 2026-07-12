package searchindex

import (
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2/search"
)

func TestBoundedFieldTermPositionsPreservesMinimumWindow(t *testing.T) {
	alpha := append(locationRanges(1, 101, 1000, 1101), &search.Location{Pos: 500})
	beta := append(locationRanges(200, 301, 2000, 2101), &search.Location{Pos: 501})
	noise := locationRanges(3000, 3200)
	positions := boundedFieldTermPositions(
		SearchRequest{Terms: []string{"alpha", "beta"}},
		search.TermLocationMap{"alpha": alpha, "beta": beta, "noise": noise},
	)
	for term, values := range positions {
		if len(values) > maximumTermPositionsPerField || !slices.IsSorted(values) {
			t.Fatalf("%s positions = %v", term, values)
		}
	}
	if !slices.Contains(positions["alpha"], 500) ||
		!slices.Contains(positions["beta"], 501) {
		t.Fatalf("minimum-window witness lost: %v / %v", positions["alpha"], positions["beta"])
	}
	if positions["noise"][0] != 3000 || positions["noise"][len(positions["noise"])-1] != 3199 {
		t.Fatalf("bounded non-query positions lost endpoints: %v", positions["noise"])
	}
}

func TestRankingPositionTermsMatchesLexicalSelection(t *testing.T) {
	content := rankingPositionTerms(SearchRequest{
		Terms: []string{" The ", "Alpha", "alpha", "и", "Beta", " "},
	})
	if !slices.Equal(content, []string{"alpha", "beta"}) {
		t.Fatalf("content terms = %v", content)
	}
	allStopwords := rankingPositionTerms(SearchRequest{Query: "the и the"})
	if !slices.Equal(allStopwords, []string{"the", "и"}) {
		t.Fatalf("all-stopword terms = %v", allStopwords)
	}
	if terms := rankingPositionTerms(SearchRequest{}); len(terms) != 0 {
		t.Fatalf("empty terms = %v", terms)
	}
}

func TestMinimumRangeWitnessesHandlesEmptyAndSingleTerm(t *testing.T) {
	if witness := minimumRangeWitnesses(map[string][]int{"alpha": {1}}, nil); witness != nil {
		t.Fatalf("empty witness = %v", witness)
	}
	witness := minimumRangeWitnesses(
		map[string][]int{"alpha": {7, 9}},
		[]string{"missing", "alpha"},
	)
	if len(witness) != 1 || witness["alpha"] != 7 {
		t.Fatalf("single-term witness = %v", witness)
	}
}

func TestBoundedTermPositionsRetainsSampledWitness(t *testing.T) {
	positions := make([]int, maximumTermPositionsPerField+10)
	for index := range positions {
		positions[index] = index
	}
	sampled := boundedTermPositions(positions, positions[1], true)
	if len(sampled) != maximumTermPositionsPerField || !slices.Contains(sampled, positions[1]) {
		t.Fatalf("sampled witness = %v", sampled)
	}
	if got := boundedTermPositions([]int{1, 2}, 2, true); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("short positions = %v", got)
	}
}

func locationRanges(bounds ...int) search.Locations {
	locations := make(search.Locations, 0)
	for index := 0; index+1 < len(bounds); index += 2 {
		for position := bounds[index]; position < bounds[index+1]; position++ {
			locations = append(locations, &search.Location{Pos: storedLocationCoordinate(position)})
		}
	}
	if len(bounds)%2 != 0 {
		locations = append(locations, &search.Location{
			Pos: storedLocationCoordinate(bounds[len(bounds)-1]),
		})
	}

	return locations
}
