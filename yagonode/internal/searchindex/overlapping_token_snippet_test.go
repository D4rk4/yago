package searchindex

import (
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const chineseFiller = "山水风云"

func TestChineseSnippetKeepsAnOverlappingWordWholeAtTheWindowEdge(t *testing.T) {
	body := strings.Repeat(chineseFiller, 10) + "天气，晴朗" + strings.Repeat(chineseFiller, 70) +
		"气晴朗。" + strings.Repeat(chineseFiller, 30)
	snippet := chineseEvidenceSnippet(t, body)
	if !strings.HasPrefix(snippet, "… ") || !strings.HasSuffix(snippet, "气晴朗") {
		t.Fatalf("snippet = %q", snippet)
	}
}

func TestChineseSnippetFallsBackWhenTheOverlappingWordCannotFit(t *testing.T) {
	body := strings.Repeat(chineseFiller, 10) + "天气，晴朗" + strings.Repeat(chineseFiller, 78) +
		"日气晴朗。" + strings.Repeat(chineseFiller, 30)
	snippet := chineseEvidenceSnippet(t, body)
	if !strings.HasPrefix(snippet, strings.Repeat(chineseFiller, 10)+"天气，晴朗") ||
		strings.Contains(snippet, "日气") {
		t.Fatalf("snippet = %q", snippet)
	}
}

func TestChineseSnippetWitnessRepeatsForIdenticalInput(t *testing.T) {
	body := "气天气"
	document := documentstore.Document{Title: "Report", ExtractedText: body, Language: "zh"}
	request := SearchRequest{Terms: []string{"天气晴朗"}, IncludePositions: true}
	analyzer := detectDocumentAnalyzer(analyzerDetectionText(document), document.Language)
	stored, err := storedDocumentLocations(t.Context(), document, request, analyzer)
	if err != nil {
		t.Fatalf("storedDocumentLocations: %v", err)
	}
	first, _ := fieldSnippetEvidence(stored.locations["body"], []string{body}, nil, true)
	for range 50 {
		if again, _ := fieldSnippetEvidence(
			stored.locations["body"],
			[]string{body},
			nil,
			true,
		); again.evidence != first.evidence {
			t.Fatalf("witness %v then %v", first.evidence, again.evidence)
		}
	}
}

func TestFieldSnippetEvidenceEndsAtTheWidestOccurrenceInsideTheWindow(t *testing.T) {
	for _, test := range []struct {
		name  string
		terms search.TermLocationMap
		want  TextQueryEvidence
	}{
		{
			name: "enclosing occurrence",
			terms: search.TermLocationMap{
				"enclosing": {{Start: 0, End: 30}},
				"nested":    {{Start: 3, End: 6}},
			},
			want: TextQueryEvidence{Start: 0, End: 30},
		},
		{
			name: "enclosing occurrence left behind",
			terms: search.TermLocationMap{
				"enclosing": {{Start: 0, End: 30}, {Start: 6, End: 9}},
				"nested":    {{Start: 3, End: 6}},
			},
			want: TextQueryEvidence{Start: 3, End: 9},
		},
		{
			name: "adjacent occurrence after the end",
			terms: search.TermLocationMap{
				"first":  {{Start: 0, End: 3}, {Start: 6, End: 9}},
				"second": {{Start: 3, End: 6}, {Start: 9, End: 12}},
			},
			want: TextQueryEvidence{Start: 0, End: 6},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			witness, found := fieldSnippetEvidence(
				test.terms,
				[]string{strings.Repeat("x", 40)},
				nil,
				true,
			)
			if !found || witness.evidence != test.want {
				t.Fatalf("witness = %v, want %v", witness.evidence, test.want)
			}
		})
	}
}

func chineseEvidenceSnippet(t *testing.T, body string) string {
	t.Helper()
	evidence, _, available, err := AnalyzeDocumentQueryEvidence(
		t.Context(),
		documentstore.Document{Title: "Report", ExtractedText: body, Language: "zh"},
		[]string{"天气晴朗"},
		MaximumDocumentQueryEvidenceBytes,
	)
	if err != nil || !available || evidence.Analyzer != cjkChineseTextAnalyzer {
		t.Fatalf("analyzer=%q available=%v err=%v", evidence.Analyzer, available, err)
	}

	return evidence.Snippet
}
