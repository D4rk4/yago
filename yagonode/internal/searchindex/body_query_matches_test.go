package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2/search"
)

func TestBoundedBodyQueryMatchesSortsCompactsAndSamples(t *testing.T) {
	text := make([]byte, 400)
	for index := range text {
		text[index] = 'x'
	}
	locations := make(search.Locations, 0, 202)
	locations = append(locations, nil, &search.Location{Start: 4, End: 4})
	for index := 199; index >= 0; index-- {
		locations = append(locations, &search.Location{
			Start: uint64(index),
			End:   uint64(index + 1),
		})
	}
	locations = append(locations, &search.Location{Start: 0, End: 1})
	matches := boundedBodyQueryMatches(string(text), search.TermLocationMap{
		"term": locations,
	})
	if len(matches) != maximumAnalyzedQueryMatches ||
		matches[0] != (TextQueryMatch{Start: 0, End: 1}) ||
		matches[len(matches)-1] != (TextQueryMatch{Start: 199, End: 200}) {
		t.Fatalf("matches = %#v", matches)
	}
	for index := 1; index < len(matches); index++ {
		if matches[index].Start <= matches[index-1].Start {
			t.Fatalf("matches not strictly ordered: %#v", matches)
		}
	}
}

func TestBodyQueryMatchRejectsInvalidLocationsAndUTF8Splits(t *testing.T) {
	for _, location := range []*search.Location{
		nil,
		{Start: 1, End: 1},
		{Start: 0, End: 100},
		{Start: 0, End: 1},
	} {
		if match, valid := bodyQueryMatch("é", location); valid {
			t.Fatalf("invalid body match = %#v", match)
		}
	}
	if match, valid := bodyQueryMatch(
		"é",
		&search.Location{Start: 0, End: 2},
	); !valid ||
		match != (TextQueryMatch{Start: 0, End: 2}) {
		t.Fatalf("valid body match = %#v, %t", match, valid)
	}
	if got := compactBodyQueryMatches(nil); got != nil {
		t.Fatalf("nil compacted = %#v", got)
	}
	one := []TextQueryMatch{{Start: 1, End: 2}}
	if got := compactBodyQueryMatches(one); len(got) != 1 || got[0] != one[0] {
		t.Fatalf("single compacted = %#v", got)
	}
}
