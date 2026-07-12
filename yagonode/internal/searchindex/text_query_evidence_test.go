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
