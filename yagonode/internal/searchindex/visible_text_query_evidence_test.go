package searchindex

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2/mapping"
)

func TestVisibleTextQueryEvidenceUsesLanguageAnalyzerAndRawRequirements(t *testing.T) {
	visible := VisibleText{
		Title:   "Президент России",
		Snippet: "Чрезвычайных полномочий передали президенту.",
		URL:     "https://example.test/politics",
	}
	evidence, available, err := AnalyzeVisibleTextQueryEvidence(
		t.Context(),
		[]string{"Чрезвычайные", "полномочия"},
		"ru",
		visible,
	)
	if err != nil {
		t.Fatalf("AnalyzeVisibleTextQueryEvidence: %v", err)
	}
	if !available || evidence.Analyzer != "ru" {
		t.Fatalf("available=%v analyzer=%q", available, evidence.Analyzer)
	}
	positions := evidence.FieldTermPositions["snippet"]
	if len(positions["чрезвычайные"]) == 0 || len(positions["полномочия"]) == 0 {
		t.Fatalf("snippet positions = %#v", positions)
	}
	if len(evidence.QueryMatches) != 2 {
		t.Fatalf("query matches = %#v", evidence.QueryMatches)
	}
	for _, match := range evidence.QueryMatches {
		if match.Start < 0 || match.End <= match.Start || match.End > len(visible.Snippet) {
			t.Fatalf("invalid query match = %#v", match)
		}
	}
}

func TestVisibleTextQueryEvidenceSupportsArabicAndHebrewNormalization(t *testing.T) {
	tests := []struct {
		name     string
		term     string
		text     string
		language string
	}{
		{name: "arabic", term: "كتاب", text: "قراءة الكتاب مفيدة", language: "ar"},
		{name: "hebrew", term: "שׁלום", text: "שׁלום עולם", language: "he"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, available, err := AnalyzeVisibleTextQueryEvidence(
				t.Context(),
				[]string{test.term},
				test.language,
				VisibleText{Snippet: test.text},
			)
			if err != nil {
				t.Fatalf("AnalyzeVisibleTextQueryEvidence: %v", err)
			}
			if !available || len(evidence.FieldTermPositions["snippet"][test.term]) == 0 ||
				len(evidence.QueryMatches) == 0 {
				t.Fatalf("available=%v evidence=%#v", available, evidence)
			}
		})
	}
}

func TestVisibleTextQueryEvidenceKeepsCJKExactEvidence(t *testing.T) {
	evidence, available, err := AnalyzeVisibleTextQueryEvidence(
		t.Context(),
		[]string{"東京", "大阪"},
		"ja",
		VisibleText{Snippet: "東京都"},
	)
	if err != nil {
		t.Fatalf("AnalyzeVisibleTextQueryEvidence: %v", err)
	}
	if !available || evidence.Analyzer != cjkJapaneseTextAnalyzer {
		t.Fatalf("available=%v analyzer=%q", available, evidence.Analyzer)
	}
	positions := evidence.FieldTermPositions["snippet"]
	if len(positions["東京"]) != 1 || len(positions["大阪"]) != 0 {
		t.Fatalf("snippet positions = %#v", positions)
	}
	if len(evidence.QueryMatches) != 1 ||
		visibleMatch("東京都", evidence.QueryMatches[0]) != "東京" {
		t.Fatalf("query matches = %#v", evidence.QueryMatches)
	}
}

func TestVisibleTextQueryEvidenceBoundsRequirementsPositionsMatchesAndText(t *testing.T) {
	terms := make([]string, maximumVisibleTextQueryRequirements+8)
	for index := range terms {
		terms[index] = fmt.Sprintf("term%d", index)
	}
	repeated := strings.Repeat("term0 term1 ", maximumAnalyzedQueryMatches+80)
	snippet := repeated + strings.Repeat("x", maximumVisibleSnippetBytes) + " term39"
	evidence, available, err := AnalyzeVisibleTextQueryEvidence(
		t.Context(),
		terms,
		"en",
		VisibleText{Snippet: snippet},
	)
	if err != nil {
		t.Fatalf("AnalyzeVisibleTextQueryEvidence: %v", err)
	}
	if !available {
		t.Fatal("visible evidence unavailable")
	}
	positions := evidence.FieldTermPositions["snippet"]
	if len(positions) != maximumVisibleTextQueryRequirements {
		t.Fatalf("requirements = %d", len(positions))
	}
	if len(positions["term0"]) != maximumTermPositionsPerField {
		t.Fatalf("positions = %d", len(positions["term0"]))
	}
	if len(evidence.QueryMatches) != maximumAnalyzedQueryMatches {
		t.Fatalf("query matches = %d", len(evidence.QueryMatches))
	}
	if _, found := positions["term39"]; found {
		t.Fatal("requirement beyond the cap was retained")
	}
}

func TestVisibleTextQueryEvidenceRejectsInvalidInputAndUnavailableAnalyzer(t *testing.T) {
	invalid := VisibleText{Snippet: "alpha\xffbeta"}
	if _, available, err := AnalyzeVisibleTextQueryEvidence(
		t.Context(),
		[]string{"alpha"},
		"en",
		invalid,
	); err != nil || available {
		t.Fatalf("invalid input available=%v err=%v", available, err)
	}
	if _, available, err := AnalyzeVisibleTextQueryEvidence(
		t.Context(),
		nil,
		"en",
		VisibleText{Snippet: "alpha"},
	); err != nil || available {
		t.Fatalf("empty query available=%v err=%v", available, err)
	}
	if _, available, err := AnalyzeVisibleTextQueryEvidence(
		t.Context(),
		[]string{"alpha"},
		"en",
		VisibleText{},
	); err != nil || available {
		t.Fatalf("empty visible text available=%v err=%v", available, err)
	}

	original := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = original })
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	if _, available, err := AnalyzeVisibleTextQueryEvidence(
		t.Context(),
		[]string{"alpha"},
		"en",
		VisibleText{Snippet: "alpha"},
	); err != nil || available {
		t.Fatalf("unavailable analyzer available=%v err=%v", available, err)
	}
}

func TestVisibleTextQueryEvidenceHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, available, err := AnalyzeVisibleTextQueryEvidence(
		ctx,
		[]string{"alpha"},
		"en",
		VisibleText{Snippet: "alpha"},
	)
	if available || !errors.Is(err, context.Canceled) {
		t.Fatalf("available=%v err=%v", available, err)
	}
	query, available := NewVisibleTextQuery([]string{"alpha"})
	if !available {
		t.Fatal("visible query unavailable")
	}
	_, available, err = query.Analyze(
		ctx,
		"en",
		VisibleText{Snippet: "alpha"},
	)
	if available || !errors.Is(err, context.Canceled) {
		t.Fatalf("prepared available=%v err=%v", available, err)
	}
}

func TestPreparedVisibleTextQueryReusesAnalyzersAcrossVisibleFields(t *testing.T) {
	query, available := NewVisibleTextQuery([]string{" ", "Alpha", "alpha", " beta "})
	if !available || len(query.requirements) != 3 {
		t.Fatalf("available=%v requirements=%#v", available, query)
	}
	first, available, err := query.Analyze(
		t.Context(),
		"",
		VisibleText{Title: "Русский заголовок alpha beta", Snippet: "short"},
	)
	if err != nil || !available || first.Analyzer != "ru" ||
		!slices.Equal(first.EvidenceRequirementOrdinals, []int{0, 2}) ||
		len(first.FieldTermPositions["title"]["alpha"]) == 0 {
		t.Fatalf("first available=%v evidence=%#v err=%v", available, first, err)
	}
	second, available, err := query.Analyze(
		t.Context(),
		"",
		VisibleText{URL: "https://example.test/alpha/beta"},
	)
	if err != nil || !available || second.Analyzer != searchTextAnalyzer ||
		len(second.FieldTermPositions["url"]["beta"]) == 0 {
		t.Fatalf("second available=%v evidence=%#v err=%v", available, second, err)
	}
	if len(query.matchers) != 2 {
		t.Fatalf("prepared analyzers = %d", len(query.matchers))
	}
	if _, available := NewVisibleTextQuery([]string{" ", "\t"}); available {
		t.Fatal("blank prepared query was available")
	}
}

func TestVisibleTextQuerySelectsScriptQualifiedAnalyzer(t *testing.T) {
	query, available := NewVisibleTextQuery([]string{"گەڕان"})
	if !available {
		t.Fatal("visible query unavailable")
	}
	evidence, available, err := query.Analyze(
		t.Context(),
		"ku-IQ",
		VisibleText{Snippet: "گەڕان بە زمانی کوردی"},
	)
	if err != nil || !available || evidence.Analyzer != "ckb" {
		t.Fatalf("available=%v analyzer=%q err=%v", available, evidence.Analyzer, err)
	}
	if analyzer := visibleTextAnalyzer("Русский текст", "de"); analyzer != "ru" {
		t.Fatalf("conflicting hint analyzer = %q", analyzer)
	}
	if analyzer := visibleTextAnalyzer("123", "de"); analyzer != "de" {
		t.Fatalf("scriptless hint analyzer = %q", analyzer)
	}
}

func TestVisibleTextBoundsPreserveUTF8AndRequirementIdentity(t *testing.T) {
	text := strings.Repeat("я", maximumVisibleTitleBytes)
	bounded := boundedVisibleTextField(text, maximumVisibleTitleBytes-1)
	if len(bounded) > maximumVisibleTitleBytes-1 || !strings.HasPrefix(text, bounded) {
		t.Fatalf("bounded text bytes = %d", len(bounded))
	}
	requirements := boundedVisibleTextRequirements([]string{" Alpha ", "alpha", " ", "Beta"})
	if !slices.Equal(requirements, []string{"Alpha", "alpha", "Beta"}) {
		t.Fatalf("requirements = %#v", requirements)
	}
}

func TestVisibleTextQueryCancellationDuringFieldScan(t *testing.T) {
	query, available := NewVisibleTextQuery([]string{"alpha"})
	if !available {
		t.Fatal("visible query unavailable")
	}
	_, available, err := query.Analyze(
		&delayedCancellationContext{},
		"en",
		VisibleText{Snippet: "alpha beta"},
	)
	if available || !errors.Is(err, context.Canceled) {
		t.Fatalf("available=%v err=%v", available, err)
	}
}

func BenchmarkVisibleTextQueryEvidence(b *testing.B) {
	visible := VisibleText{
		Title:   "Чрезвычайные полномочия президента",
		Snippet: "Правовой обзор чрезвычайных полномочий и условий их применения.",
		URL:     "https://example.test/legal/emergency-powers",
	}
	terms := []string{"чрезвычайные", "полномочия"}
	query, available := NewVisibleTextQuery(terms)
	if !available {
		b.Fatal("visible query unavailable")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, available, err := query.Analyze(
			b.Context(),
			"ru",
			visible,
		); err != nil || !available {
			b.Fatalf("available=%v err=%v", available, err)
		}
	}
}

func visibleMatch(text string, match TextQueryMatch) string {
	return text[match.Start:match.End]
}

type delayedCancellationContext struct {
	checks int
}

func (c *delayedCancellationContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *delayedCancellationContext) Done() <-chan struct{} {
	return nil
}

func (c *delayedCancellationContext) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}

	return nil
}

func (c *delayedCancellationContext) Value(any) any {
	return nil
}
