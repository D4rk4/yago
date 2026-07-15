package searchindex

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2/search"
)

type cancelAfterFirstEvidenceCheck struct {
	checks int
}

func (*cancelAfterFirstEvidenceCheck) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*cancelAfterFirstEvidenceCheck) Done() <-chan struct{} {
	return nil
}

func (c *cancelAfterFirstEvidenceCheck) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}

	return nil
}

func (*cancelAfterFirstEvidenceCheck) Value(any) any {
	return nil
}

func TestFindTextQueryEvidenceMatchesRussianMorphology(t *testing.T) {
	text := "Хроника рассказывает о псилобатах и дальних переходах."
	evidence, found := FindTextQueryEvidence(
		t.Context(),
		text,
		[]string{"псилобаты"},
		"ru",
	)
	if !found || text[evidence.Start:evidence.End] != "псилобатах" {
		t.Fatalf("evidence = %#v, found = %v", evidence, found)
	}
}

func TestFindTextQueryEvidencePrefersRussianContentOverWrongLanguage(t *testing.T) {
	if _, found := FindTextQueryEvidence(
		t.Context(),
		"О псилобатах.",
		[]string{"псилобаты"},
		"en",
	); !found {
		t.Fatal("Russian morphology rejected because of the peer language label")
	}
}

func TestFindTextQueryEvidenceRejectsDifferentRussianStem(t *testing.T) {
	if evidence, found := FindTextQueryEvidence(
		t.Context(),
		"Хроника рассказывает о психопатах.",
		[]string{"псилобаты"},
		"ru",
	); found {
		t.Fatalf("unexpected evidence = %#v", evidence)
	}
}

func TestFindTextQueryEvidenceRequiresEveryTermInOneAnalyzerBranch(t *testing.T) {
	text := "Псилобаты пересекали море. Позже псилобатах помогали корабли."
	evidence, found := FindTextQueryEvidence(
		t.Context(),
		text,
		[]string{"псилобаты", "корабль"},
		"ru",
	)
	if !found {
		t.Fatal("morphological evidence not found")
	}
	witness := text[evidence.Start:evidence.End]
	if !strings.Contains(witness, "псилобатах") || !strings.Contains(witness, "корабли") {
		t.Fatalf("witness = %q", witness)
	}
	if _, found := FindTextQueryEvidence(
		t.Context(),
		text,
		[]string{"псилобаты", "дирижабль"},
		"ru",
	); found {
		t.Fatal("partial term coverage admitted")
	}
}

func TestFindTextQueryEvidenceHandlesExactAndInvalidInputs(t *testing.T) {
	if _, found := FindTextQueryEvidence(
		t.Context(),
		"YaGoSeek exact token",
		[]string{"YaGoSeek"},
		"",
	); !found {
		t.Fatal("standard exact evidence not found")
	}
	if _, found := FindTextQueryEvidence(t.Context(), "", []string{"term"}, ""); found {
		t.Fatal("empty text admitted")
	}
	if _, found := FindTextQueryEvidence(t.Context(), "term", nil, ""); found {
		t.Fatal("empty terms admitted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, found := FindTextQueryEvidence(canceled, "term", []string{"term"}, ""); found {
		t.Fatal("canceled evidence scan admitted")
	}
	if _, found := FindTextQueryEvidence(
		&cancelAfterFirstEvidenceCheck{},
		"term",
		[]string{"term"},
		"",
	); found {
		t.Fatal("mid-scan cancellation admitted")
	}
	if _, found := FindTextQueryEvidence(t.Context(), "и", []string{"и"}, "ru"); !found {
		t.Fatal("standard branch did not retain an analyzer stopword")
	}
}

func TestFindTextQueryEvidenceHandlesRepetitivePage(t *testing.T) {
	text := strings.Repeat("псилобатах кораблях ", 4_096)
	if _, found := FindTextQueryEvidence(
		t.Context(),
		text,
		[]string{"псилобаты", "корабль"},
		"en",
	); !found {
		t.Fatal("repetitive evidence not found")
	}
}

func TestFindTextQueryEvidenceAssignsCollapsedStemOccurrencesOnce(t *testing.T) {
	if evidence, found := FindTextQueryEvidence(
		t.Context(),
		"game",
		[]string{"gaming", "games"},
		"en",
	); found {
		t.Fatalf("one occurrence admitted two requirements: %#v", evidence)
	}
	if _, found := FindTextQueryEvidence(
		t.Context(),
		"game game",
		[]string{"gaming", "games"},
		"en",
	); !found {
		t.Fatal("two occurrences did not satisfy two collapsed requirements")
	}
}

func TestMinimumTargetTextQueryEvidenceRequiresEveryAnalyzedComponent(t *testing.T) {
	matcher := &storedEvidenceMatcher{
		lookup: map[string][]int{},
		cache:  map[string]storedTokenEvidence{},
	}
	matcher.addTarget("compound", "alpha")
	matcher.addTarget("compound", "beta")
	matcher.queries = len(matcher.required)
	field, err := scanStoredFieldEvidence(
		t.Context(),
		matcher,
		[]string{"alpha beta"},
		true,
	)
	if err != nil {
		t.Fatalf("scanStoredFieldEvidence: %v", err)
	}
	if _, found := minimumTargetTextQueryEvidence(
		"alpha beta",
		matcher,
		field.targetTerms,
	); !found {
		t.Fatal("complete analyzed components were rejected")
	}
	field, err = scanStoredFieldEvidence(
		t.Context(),
		matcher,
		[]string{"alpha"},
		true,
	)
	if err != nil {
		t.Fatalf("scanStoredFieldEvidence partial: %v", err)
	}
	if evidence, found := minimumTargetTextQueryEvidence(
		"alpha",
		matcher,
		field.targetTerms,
	); found {
		t.Fatalf("partial analyzed components admitted: %#v", evidence)
	}
}

func TestMinimumTargetTextQueryEvidenceRejectsInvalidLocationsAndOrdersEqualStarts(
	t *testing.T,
) {
	matcher := &storedEvidenceMatcher{targets: make([]storedEvidenceTarget, 1)}
	invalid := map[int]search.Locations{
		0: {
			nil,
			{Start: 2, End: 1},
			{Start: 0, End: 100},
		},
	}
	if evidence, found := minimumTargetTextQueryEvidence(
		"text",
		matcher,
		invalid,
	); found {
		t.Fatalf("invalid target evidence = %#v", evidence)
	}
	matcher.targets = make([]storedEvidenceTarget, 2)
	evidence, found := minimumTargetTextQueryEvidence(
		"ab",
		matcher,
		map[int]search.Locations{
			0: {{Start: 0, End: 2}},
			1: {{Start: 0, End: 1}},
		},
	)
	if !found || evidence != (TextQueryEvidence{Start: 0, End: 2}) {
		t.Fatalf("target evidence = %#v/%t", evidence, found)
	}
}

func TestMinimumTextQueryEvidenceRejectsInvalidLocations(t *testing.T) {
	matcher := &storedEvidenceMatcher{required: []string{"term"}}
	locations := search.TermLocationMap{
		"term": search.Locations{
			nil,
			{Start: 2, End: 1},
			{Start: 0, End: 100},
		},
	}
	if evidence, found := minimumTextQueryEvidence("text", matcher, locations); found {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func TestMinimumTextQueryEvidenceOrdersEqualStartsByEnd(t *testing.T) {
	matcher := &storedEvidenceMatcher{required: []string{"first", "second"}}
	locations := search.TermLocationMap{
		"first":  search.Locations{{Start: 0, End: 2}},
		"second": search.Locations{{Start: 0, End: 1}},
	}
	evidence, found := minimumTextQueryEvidence("ab", matcher, locations)
	if !found || evidence != (TextQueryEvidence{Start: 0, End: 2}) {
		t.Fatalf("evidence = %#v, found = %v", evidence, found)
	}
}

func TestMinimumTextQueryEvidenceOrdersDifferentStartsAndRejectsMissingTerm(t *testing.T) {
	matcher := &storedEvidenceMatcher{required: []string{"first", "second"}}
	evidence, found := minimumTextQueryEvidence(
		"a b",
		matcher,
		search.TermLocationMap{
			"first":  {{Start: 2, End: 3}},
			"second": {{Start: 0, End: 1}},
		},
	)
	if !found || evidence != (TextQueryEvidence{Start: 0, End: 3}) {
		t.Fatalf("evidence = %#v/%t", evidence, found)
	}
	if evidence, found := minimumTextQueryEvidence(
		"a",
		matcher,
		search.TermLocationMap{"first": {{Start: 0, End: 1}}},
	); found {
		t.Fatalf("missing term evidence = %#v", evidence)
	}
}
