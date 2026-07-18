package searchindex

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestDocumentQueryEvidenceUsesStoredFieldsAndAbsoluteBodyOffsets(t *testing.T) {
	body := strings.Repeat("введение ", 80) + "чрезвычайных полномочий передали президенту"
	document := documentstore.Document{
		NormalizedURL: "https://example.test/чрезвычайные-полномочия",
		Title:         "Президент России",
		Headings:      []string{"Чрезвычайные меры"},
		ExtractedText: body,
		Language:      "ru",
		Inlinks:       []documentstore.AnchorText{{Text: "полномочия президента"}},
	}
	evidence, analyzedBytes, available, err := AnalyzeDocumentQueryEvidence(
		t.Context(),
		document,
		[]string{"чрезвычайные", "полномочия"},
		MaximumDocumentQueryEvidenceBytes,
	)
	if err != nil {
		t.Fatalf("AnalyzeDocumentQueryEvidence: %v", err)
	}
	if !available || evidence.Analyzer != "ru" || analyzedBytes == 0 {
		t.Fatalf("available=%v analyzer=%q bytes=%d", available, evidence.Analyzer, analyzedBytes)
	}
	bodyPositions := evidence.FieldRequirementPositions["body"]
	if len(bodyPositions[0]) == 0 || len(bodyPositions[1]) == 0 {
		t.Fatalf("body positions = %#v", bodyPositions)
	}
	if len(evidence.BodyMatches) != 2 {
		t.Fatalf("body matches = %#v", evidence.BodyMatches)
	}
	for _, match := range evidence.BodyMatches {
		if match.Start < strings.Index(body, "чрезвычайных") || match.End > len(body) {
			t.Fatalf("body match is not absolute: %#v", match)
		}
	}
	if !strings.Contains(evidence.Snippet, "чрезвычайных полномочий") ||
		len(evidence.SnippetMatches) != 2 {
		t.Fatalf("snippet=%q matches=%#v", evidence.Snippet, evidence.SnippetMatches)
	}
	if len(evidence.FieldRequirementPositions["headings"][0]) == 0 ||
		len(evidence.FieldRequirementPositions["anchors"][1]) == 0 ||
		len(evidence.FieldRequirementPositions["url"][0]) == 0 {
		t.Fatalf("stored field positions = %#v", evidence.FieldRequirementPositions)
	}
}

func TestDocumentQueryEvidenceBoundsSourceAndRejectsInvalidText(t *testing.T) {
	prefix := strings.Repeat("x", MaximumDocumentQueryEvidenceBytes+32)
	evidence, analyzedBytes, available, err := AnalyzeDocumentQueryEvidence(
		t.Context(),
		documentstore.Document{ExtractedText: prefix + " target"},
		[]string{"target"},
		MaximumDocumentQueryEvidenceBytes+1,
	)
	if err != nil || !available || analyzedBytes != MaximumDocumentQueryEvidenceBytes ||
		len(evidence.BodyMatches) != 0 {
		t.Fatalf(
			"evidence=%#v bytes=%d available=%v err=%v",
			evidence,
			analyzedBytes,
			available,
			err,
		)
	}
	_, analyzedBytes, available, err = AnalyzeDocumentQueryEvidence(
		t.Context(),
		documentstore.Document{Title: "bad\xff"},
		[]string{"bad"},
		100,
	)
	if err != nil || available || analyzedBytes != 0 {
		t.Fatalf("invalid UTF-8 bytes=%d available=%v err=%v", analyzedBytes, available, err)
	}
}

func TestDocumentQueryEvidenceHonorsCancellationAndAvailability(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, _, err := AnalyzeDocumentQueryEvidence(
		ctx,
		documentstore.Document{ExtractedText: "target"},
		[]string{"target"},
		100,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if !StoredEvidenceAnalyzerAvailable("ru") || StoredEvidenceAnalyzerAvailable("missing") {
		t.Fatal("stored analyzer availability mismatch")
	}
	visible := VisibleText{Snippet: "чрезвычайных полномочий"}
	if !StoredEvidenceAnalyzerCompatible("ru", "ru", visible) ||
		!StoredEvidenceAnalyzerCompatible("en", "en", VisibleText{
			Title:   "Metadata title",
			Snippet: "x",
		}) ||
		!StoredEvidenceAnalyzerCompatible("en", "en", VisibleText{
			URL: "https://example.test/report",
		}) ||
		StoredEvidenceAnalyzerCompatible("en", "ru", visible) ||
		StoredEvidenceAnalyzerCompatible("missing", "ru", visible) ||
		StoredEvidenceAnalyzerCompatible("ru", "ru", VisibleText{Snippet: "bad\xff"}) {
		t.Fatal("stored analyzer compatibility mismatch")
	}
	_, analyzedBytes, available, err := AnalyzeDocumentQueryEvidence(
		t.Context(),
		documentstore.Document{ExtractedText: "target"},
		nil,
		100,
	)
	if err != nil || available || analyzedBytes != 0 {
		t.Fatalf("empty requirements bytes=%d available=%v err=%v", analyzedBytes, available, err)
	}
}

func TestDocumentQueryEvidencePropagatesCancellationAtEveryAnalysisSurface(t *testing.T) {
	cases := []struct {
		name     string
		document documentstore.Document
		cancelAt int
		want     string
	}{
		{
			name:     "stored document",
			document: documentstore.Document{Title: "term"},
			cancelAt: 2,
			want:     "analyze document query evidence",
		},
		{
			name: "URL",
			document: documentstore.Document{
				NormalizedURL: "https://example.test/term",
			},
			cancelAt: 2,
			want:     "analyze document URL evidence",
		},
		{
			name:     "snippet",
			document: documentstore.Document{Title: "term"},
			cancelAt: 3,
			want:     "analyze document snippet evidence",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			staged := &stagedEvidenceCancellationContext{
				Context:  ctx,
				cancel:   cancel,
				cancelAt: test.cancelAt,
			}
			_, _, available, err := AnalyzeDocumentQueryEvidence(
				staged,
				test.document,
				[]string{"term"},
				1024,
			)
			if available || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("available=%v err=%v", available, err)
			}
		})
	}
}

func TestDocumentQueryEvidenceInternalBoundsAndOrdering(t *testing.T) {
	matcher := newStoredEvidenceMatcher(SearchRequest{Terms: []string{"powers"}}, "en")
	requirement := matcher.rawRequirements[0]
	analyzed := matcher.rawRequirementAnalyzedTerms[requirement][0]
	locations := evidenceRequirementLocations(requirement, matcher, map[string]search.Locations{
		analyzed: {{Pos: 2}, {Pos: 1}},
	})
	if len(locations) != 2 || locations[0].Pos != 1 || locations[1].Pos != 2 {
		t.Fatalf("locations = %#v", locations)
	}
	document := documentstore.Document{ExtractedText: "fallback target text"}
	want := queryBiasedSnippet(document.ExtractedText, []string{"target"}, "")
	for _, match := range []TextQueryMatch{
		{Start: -1, End: 1},
		{Start: 0, End: len(document.ExtractedText) + 1},
		{Start: 1, End: 1},
	} {
		if got := queryEvidenceSnippet(
			document,
			[]string{"target"},
			[]TextQueryMatch{match},
		); got != want {
			t.Fatalf("match=%#v snippet=%q want=%q", match, got, want)
		}
	}
	for _, test := range []struct {
		text      string
		remaining int
		valid     bool
		wantText  string
		wantLeft  int
		wantValid bool
	}{
		{text: "term", remaining: 0, valid: true, wantText: "", wantLeft: 0, wantValid: true},
		{text: "éx", remaining: 1, valid: true, wantText: "", wantLeft: 0, wantValid: true},
		{text: "term", remaining: 4, valid: false, wantText: "", wantLeft: 4, wantValid: false},
	} {
		text, remaining, valid := boundedEvidenceText(test.text, test.remaining, test.valid)
		if !reflect.DeepEqual(
			[]any{text, remaining, valid},
			[]any{test.wantText, test.wantLeft, test.wantValid},
		) {
			t.Fatalf("bounded text = %q %d %v", text, remaining, valid)
		}
	}
}

type stagedEvidenceCancellationContext struct {
	context.Context
	cancel   context.CancelFunc
	calls    int
	cancelAt int
}

func (c *stagedEvidenceCancellationContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		c.cancel()
	}

	err := c.Context.Err()
	if err == nil {
		return nil
	}

	return fmt.Errorf("staged evidence context: %w", err)
}

func BenchmarkDocumentQueryEvidence(b *testing.B) {
	document := documentstore.Document{
		NormalizedURL: "https://example.test/report",
		Title:         "Чрезвычайные полномочия",
		Headings:      []string{"Официальный отчет"},
		ExtractedText: strings.Repeat("обычный текст ", 400) + "чрезвычайных полномочий",
		Language:      "ru",
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _, available, err := AnalyzeDocumentQueryEvidence(
			b.Context(),
			document,
			[]string{"чрезвычайные", "полномочия"},
			MaximumDocumentQueryEvidenceBytes,
		)
		if err != nil || !available {
			b.Fatalf("available=%v err=%v", available, err)
		}
	}
}
