package searchindex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
)

func TestStoredEvidenceMatcherDegradesWithoutAnalyzerMapping(t *testing.T) {
	original := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = original })
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	if storedEvidenceAnalyzer("missing") != nil {
		t.Fatal("missing mapping returned a phrase analyzer")
	}

	matcher := newStoredEvidenceMatcher(
		SearchRequest{Terms: []string{" ", "Needle", "needle"}},
		"missing",
	)
	if matcher.queries != 1 || len(matcher.targets) != 1 ||
		len(matcher.match("NEEDLE")) != 1 {
		t.Fatalf("matcher = %#v", matcher)
	}
	matcher.addTarget("ignored", "")
	matcher.addTarget("needle", "needle")
	if len(matcher.targets) != 1 {
		t.Fatalf("duplicate targets = %#v", matcher.targets)
	}
}

func TestStoredEvidenceMatcherFallsBackFromUnavailableAndDroppingAnalyzers(t *testing.T) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatalf("newSearchIndexMapping: %v", err)
	}
	original := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = original })
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return indexMapping }

	missing := newStoredEvidenceMatcher(
		SearchRequest{Terms: []string{"Needle"}},
		"missing",
	)
	if missing.analyzer == nil || len(missing.targets) != 1 {
		t.Fatalf("missing analyzer fallback = %#v", missing)
	}
	dropped := newStoredEvidenceMatcher(
		SearchRequest{Terms: []string{"the"}},
		searchTextAnalyzer,
	)
	if dropped.name != standardTextAnalyzer || len(dropped.targets) != 1 ||
		dropped.targets[0].analyzed != "the" {
		t.Fatalf("dropping analyzer fallback = %#v", dropped)
	}
}

func TestStoredCJKFieldHandlesLatinTokensSeparatorsAndCancellation(t *testing.T) {
	matcher := &storedEvidenceMatcher{
		name:   "cjk",
		lookup: map[string][]int{},
		cache:  map[string][]int{},
	}
	matcher.addTarget("go", "go")
	matcher.addTarget("日", "日")
	matcher.queries = len(matcher.required)
	terms, err := scanStoredCJKField(
		t.Context(),
		matcher,
		[]string{"GO 日1"},
		false,
	)
	if err != nil {
		t.Fatalf("scanStoredCJKField: %v", err)
	}
	if len(terms["go"]) != 1 || len(terms["日"]) != 1 || containsStoredCJK("123") {
		t.Fatalf("terms = %#v", terms)
	}
	if !containsStoredCJK("日") {
		t.Fatal("CJK text was not recognized")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = scanStoredCJKField(ctx, matcher, []string{"日本"}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan error = %v", err)
	}
}

func TestStoredCJKSequenceRetainsSingleCharacterEvidence(t *testing.T) {
	matcher := &storedEvidenceMatcher{lookup: map[string][]int{}}
	matcher.addTarget("日", "日")
	field := &storedFieldEvidence{
		terms:      search.TermLocationMap{},
		latest:     map[int]*search.Location{},
		bestSpan:   int(^uint(0) >> 1),
		queryTerms: 1,
	}
	position := field.addCJKSequence(
		storedCJKValue{matcher: matcher, text: "日", arrayLength: 1},
		0,
		len("日"),
		0,
	)
	if position != 1 || len(field.terms["日"]) != 1 {
		t.Fatalf("position=%d terms=%#v", position, field.terms)
	}

	field.terms = search.TermLocationMap{}
	field.latest = map[int]*search.Location{}
	position = field.addCJKSequence(
		storedCJKValue{matcher: matcher, text: "日1", arrayLength: 1},
		0,
		len("日1"),
		0,
	)
	if position != 1 || len(field.terms["日"]) != 1 {
		t.Fatalf("mixed position=%d terms=%#v", position, field.terms)
	}
}

func TestStoredLocationCoordinatesBoundInvalidAndArrayValues(t *testing.T) {
	location := newStoredLocation(storedLocationCoordinates{
		position: 3, start: 2, end: 4, arrayIndex: 1, arrayLength: 2,
	})
	if location.Pos != 3 || !location.ArrayPositions.Equals(search.ArrayPositions{1}) {
		t.Fatalf("location = %#v", location)
	}
	if storedLocationCoordinate(-1) != 0 {
		t.Fatal("negative coordinate was not bounded")
	}
}

func TestStoredFuzzyEvidenceRejectsPrefixAndDistanceBounds(t *testing.T) {
	matcher := &storedEvidenceMatcher{fuzzy: true}
	target := newStoredEvidenceTarget(0, "needle", "needle")
	if matcher.analyzedTermMatches("poodle", target) {
		t.Fatal("different fuzzy prefix matched")
	}
	if runeLengthDifference("a", "abc") != 2 {
		t.Fatal("negative rune difference was not normalized")
	}
	if !containsFoldedRunes("anything", nil) {
		t.Fatal("empty folded anchor did not match")
	}
	if matcher.storedEvidenceWithinDistance(
		strings.Repeat("a", maximumFuzzyTermRunes+1),
		storedEvidenceTarget{
			analyzedRunes: []rune(strings.Repeat("a", maximumFuzzyTermRunes)),
			distance:      2,
		},
	) {
		t.Fatal("oversized candidate matched")
	}
	if matcher.storedEvidenceWithinDistance(
		"a",
		storedEvidenceTarget{
			analyzedRunes: []rune(strings.Repeat("a", maximumFuzzyTermRunes+1)),
			distance:      2,
		},
	) {
		t.Fatal("oversized target matched")
	}
	if matcher.storedEvidenceWithinDistance(
		"zzzz",
		storedEvidenceTarget{analyzedRunes: []rune("aaaa"), distance: 1},
	) {
		t.Fatal("distant token matched")
	}
}

func TestStoredProximityHandlesSingleTermsNilHitsAndStableTies(t *testing.T) {
	if hitUnorderedProximity(&search.DocumentMatch{}, []string{"one"}) != 0 ||
		hitOrderedProximity(&search.DocumentMatch{}, []string{"one"}) != 0 {
		t.Fatal("single term received proximity")
	}
	if positions := hitBodyPositions(nil, []string{"one"}); len(positions) != 0 {
		t.Fatalf("nil hit positions = %#v", positions)
	}
	results := []SearchResult{
		{DocumentID: "b", Score: 1},
		{DocumentID: "a", Score: 1},
	}
	rescoreStoredProximity(results, SearchRequest{
		Terms: []string{"one", "two"}, IncludePositions: true,
	})
	if results[0].DocumentID != "a" {
		t.Fatalf("tie order = %#v", results)
	}
}

func TestStoredFieldEvidenceBoundsLocationsAndPreservesWitnesses(t *testing.T) {
	target := newStoredEvidenceTarget(0, "needle", "needle")
	field := &storedFieldEvidence{
		terms:  search.TermLocationMap{},
		latest: map[int]*search.Location{},
	}
	for position := 0; position <= maximumTermPositionsPerField; position++ {
		field.add(target, &search.Location{Pos: storedLocationCoordinate(position)})
	}
	locations := field.terms["needle"]
	if len(locations) != maximumTermPositionsPerField ||
		locations[len(locations)-1].Pos != maximumTermPositionsPerField {
		t.Fatalf("bounded locations = %#v", locations)
	}

	matcher := &storedEvidenceMatcher{required: []string{"needle"}}
	field.terms = search.TermLocationMap{"needle": {}}
	field.witnesses = map[int]*search.Location{0: {Pos: 999}}
	field.preserveWitnesses(matcher)
	if len(field.terms["needle"]) != 1 || field.terms["needle"][0].Pos != 999 {
		t.Fatalf("appended witness = %#v", field.terms["needle"])
	}

	full := make(search.Locations, maximumTermPositionsPerField)
	for index := range full {
		full[index] = &search.Location{Pos: storedLocationCoordinate(index)}
	}
	field.terms["needle"] = full
	field.preserveWitnesses(matcher)
	if field.terms["needle"][len(full)/2].Pos != 999 {
		t.Fatalf("replaced witness = %#v", field.terms["needle"])
	}
}
