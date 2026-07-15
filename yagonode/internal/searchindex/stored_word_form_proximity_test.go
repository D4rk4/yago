package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestStoredWordFormProximityKeepsExactSurfacePriority(t *testing.T) {
	terms := []string{"running", "and", "jumping"}
	exact := storedBodyWordFormEvidence(t, "en", terms, "running and jumping", false)
	inflected := storedBodyWordFormEvidence(t, "en", terms, "runs and jumps", false)
	scattered := storedBodyWordFormEvidence(
		t,
		"en",
		terms,
		"runs 1 2 3 4 5 6 7 8 9 jumps",
		false,
	)
	if exact.proximity != 1 || exact.orderedProximity != 1 {
		t.Fatalf("exact evidence = %v/%v", exact.proximity, exact.orderedProximity)
	}
	if inflected.proximity != analyzerVariantPairConfidence ||
		inflected.orderedProximity != analyzerVariantPairConfidence {
		t.Fatalf(
			"inflected evidence = %v/%v",
			inflected.proximity,
			inflected.orderedProximity,
		)
	}
	if scattered.proximity != 0 || scattered.orderedProximity != 0 {
		t.Fatalf(
			"scattered evidence = %v/%v",
			scattered.proximity,
			scattered.orderedProximity,
		)
	}
}

func TestStoredWordFormProximityPreservesOrderAndOriginalGaps(t *testing.T) {
	terms := []string{"running", "and", "jumping"}
	reverse := storedBodyWordFormEvidence(t, "en", terms, "jumps and runs", false)
	reverseScattered := storedBodyWordFormEvidence(
		t,
		"en",
		terms,
		"jumps 1 2 3 4 5 6 7 8 9 runs",
		false,
	)
	collapsedGap := storedBodyWordFormEvidence(t, "en", terms, "runs jumps", false)
	exactCollapsedGap := storedBodyWordFormEvidence(
		t,
		"en",
		terms,
		"running jumping",
		false,
	)
	if reverse.proximity != analyzerVariantPairConfidence || reverse.orderedProximity != 0 {
		t.Fatalf("reverse evidence = %v/%v", reverse.proximity, reverse.orderedProximity)
	}
	if reverseScattered.proximity != 0 || reverseScattered.orderedProximity != 0 {
		t.Fatalf(
			"reverse scattered evidence = %v/%v",
			reverseScattered.proximity,
			reverseScattered.orderedProximity,
		)
	}
	if collapsedGap.proximity != analyzerVariantPairConfidence ||
		collapsedGap.orderedProximity != 0 {
		t.Fatalf(
			"collapsed gap evidence = %v/%v",
			collapsedGap.proximity,
			collapsedGap.orderedProximity,
		)
	}
	if exactCollapsedGap.proximity != 1 || exactCollapsedGap.orderedProximity != 0 {
		t.Fatalf(
			"exact collapsed gap evidence = %v/%v",
			exactCollapsedGap.proximity,
			exactCollapsedGap.orderedProximity,
		)
	}
}

func TestStoredWordFormProximityDoesNotReuseCollapsedRequirements(t *testing.T) {
	oneOccurrence := storedBodyWordFormEvidence(
		t,
		"en",
		[]string{"gaming", "games"},
		"game",
		false,
	)
	twoOccurrences := storedBodyWordFormEvidence(
		t,
		"en",
		[]string{"gaming", "games"},
		"game game",
		false,
	)
	if oneOccurrence.proximity != 0 || oneOccurrence.orderedProximity != 0 {
		t.Fatalf(
			"one occurrence evidence = %v/%v",
			oneOccurrence.proximity,
			oneOccurrence.orderedProximity,
		)
	}
	if twoOccurrences.proximity != analyzerVariantPairConfidence ||
		twoOccurrences.orderedProximity != analyzerVariantPairConfidence {
		t.Fatalf(
			"two occurrence evidence = %v/%v",
			twoOccurrences.proximity,
			twoOccurrences.orderedProximity,
		)
	}
}

func TestStoredWordFormProximityExcludesFuzzyRequests(t *testing.T) {
	terms := []string{"running", "jumping"}
	exact := storedBodyWordFormEvidence(t, "en", terms, "running jumping", true)
	wordForms := storedBodyWordFormEvidence(t, "en", terms, "runs jumps", true)
	if exact.proximity != 1 || exact.orderedProximity != 1 {
		t.Fatalf("fuzzy exact evidence = %v/%v", exact.proximity, exact.orderedProximity)
	}
	if wordForms.proximity != 0 || wordForms.orderedProximity != 0 {
		t.Fatalf(
			"fuzzy word-form evidence = %v/%v",
			wordForms.proximity,
			wordForms.orderedProximity,
		)
	}
}

func TestStoredWordFormProximityKeepsExactPositionsPrivate(t *testing.T) {
	req := SearchRequest{
		Terms:            []string{"running", "jumping"},
		IncludePositions: true,
	}
	evidence, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: "runs jumps"},
		req,
		"en",
	)
	if err != nil {
		t.Fatalf("word-form positions: %v", err)
	}
	positions := exactSurfaceFieldTermPositions(req, evidence.exactLocations)
	if len(positions["body"]["running"]) != 0 ||
		len(positions["body"]["jumping"]) != 0 {
		t.Fatalf("analyzer-only positions became exact: %#v", positions)
	}
	if evidence.proximity != analyzerVariantPairConfidence {
		t.Fatalf("private word-form proximity = %v", evidence.proximity)
	}
}

func TestStoredWordFormProximityRespectsStandardAnalyzerAndRepeatedTerms(t *testing.T) {
	standard := storedBodyWordFormEvidence(
		t,
		standardTextAnalyzer,
		[]string{"running", "jumping"},
		"runs jumps",
		false,
	)
	repeated := storedBodyWordFormEvidence(
		t,
		"en",
		[]string{"running", "running"},
		"runs runs",
		false,
	)
	if standard.proximity != 0 || standard.orderedProximity != 0 {
		t.Fatalf(
			"standard analyzer evidence = %v/%v",
			standard.proximity,
			standard.orderedProximity,
		)
	}
	if repeated.proximity != 0 || repeated.orderedProximity != 0 {
		t.Fatalf(
			"repeated requirement evidence = %v/%v",
			repeated.proximity,
			repeated.orderedProximity,
		)
	}
}

func TestStoredWordFormProximityUsesOneFieldAndArray(t *testing.T) {
	req := SearchRequest{
		Terms:            []string{"running", "jumping"},
		IncludePositions: true,
	}
	fields, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{Title: "running", ExtractedText: "jumping"},
		req,
		"en",
	)
	if err != nil {
		t.Fatalf("field evidence: %v", err)
	}
	arrays, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{Headings: []string{"running", "jumping"}},
		req,
		"en",
	)
	if err != nil {
		t.Fatalf("array evidence: %v", err)
	}
	if fields.proximity != 0 || fields.orderedProximity != 0 {
		t.Fatalf("cross-field evidence = %v/%v", fields.proximity, fields.orderedProximity)
	}
	if arrays.proximity != 0 || arrays.orderedProximity != 0 {
		t.Fatalf("cross-array evidence = %v/%v", arrays.proximity, arrays.orderedProximity)
	}
}

func TestStoredWordFormProximitySupportsRussianInflection(t *testing.T) {
	terms := []string{"игровые", "устройства"}
	exact := storedBodyWordFormEvidence(t, "ru", terms, "игровые устройства", false)
	inflected := storedBodyWordFormEvidence(t, "ru", terms, "игровыми устройствами", false)
	scattered := storedBodyWordFormEvidence(
		t,
		"ru",
		terms,
		"игровыми 1 2 3 4 5 6 7 8 9 устройствами",
		false,
	)
	if exact.proximity != 1 || exact.orderedProximity != 1 {
		t.Fatalf("Russian exact evidence = %v/%v", exact.proximity, exact.orderedProximity)
	}
	if inflected.proximity != analyzerVariantPairConfidence ||
		inflected.orderedProximity != analyzerVariantPairConfidence {
		t.Fatalf(
			"Russian inflected evidence = %v/%v",
			inflected.proximity,
			inflected.orderedProximity,
		)
	}
	if scattered.proximity != 0 || scattered.orderedProximity != 0 {
		t.Fatalf(
			"Russian scattered evidence = %v/%v",
			scattered.proximity,
			scattered.orderedProximity,
		)
	}
}

func TestStoredWordFormProximityKeepsPossessiveTokenPositions(t *testing.T) {
	for _, apostrophe := range []string{"'", "’", "＇"} {
		phrase := "operator" + apostrophe + "s archive"
		evidence := storedBodyWordFormEvidence(
			t,
			"en",
			strings.Fields(phrase),
			phrase,
			false,
		)
		if evidence.proximity != 1 || evidence.orderedProximity != 1 {
			t.Fatalf(
				"apostrophe %q evidence = %v/%v",
				apostrophe,
				evidence.proximity,
				evidence.orderedProximity,
			)
		}
	}
	statement := "telemetry tool’s archive served its users’ entire project from cloud storage"
	compact := storedBodyWordFormEvidence(
		t,
		"en",
		strings.Fields(statement),
		statement,
		false,
	)
	scattered := storedBodyWordFormEvidence(
		t,
		"en",
		strings.Fields(statement),
		strings.Replace(
			statement,
			"users’ entire",
			"users’ 1 2 3 4 5 6 7 8 9 entire",
			1,
		),
		false,
	)
	reversed := storedBodyWordFormEvidence(
		t,
		"en",
		strings.Fields(statement),
		"storage cloud from project entire users’ its served archive tool’s telemetry",
		false,
	)
	if compact.proximity <= scattered.proximity ||
		compact.orderedProximity <= scattered.orderedProximity ||
		compact.orderedProximity <= reversed.orderedProximity {
		t.Fatalf(
			"long possessive evidence compact=%v/%v scattered=%v/%v reversed=%v/%v",
			compact.proximity,
			compact.orderedProximity,
			scattered.proximity,
			scattered.orderedProximity,
			reversed.proximity,
			reversed.orderedProximity,
		)
	}
}

func TestStoredPhraseEvidenceKeepsAnalyzerPositionGaps(t *testing.T) {
	phrase := "best mouse for gaming"
	req := SearchRequest{Terms: strings.Fields(phrase), Phrases: []string{phrase}}
	evidence, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: phrase},
		req,
		"en",
	)
	if err != nil {
		t.Fatalf("phrase evidence: %v", err)
	}
	preference := storedQuotedPhrasePreference(
		evidence.phraseLocations,
		req.Phrases,
		storedEvidenceAnalyzer("en"),
	)
	if preference != 1 {
		t.Fatalf("stopword-gap phrase preference = %v", preference)
	}
}

func storedBodyWordFormEvidence(
	t *testing.T,
	analyzer string,
	terms []string,
	text string,
	fuzzy bool,
) storedDocumentEvidence {
	t.Helper()
	evidence, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: text},
		SearchRequest{Terms: terms, IncludePositions: true, Fuzzy: fuzzy},
		analyzer,
	)
	if err != nil {
		t.Fatalf("stored document evidence: %v", err)
	}

	return evidence
}
