package searchindex

import (
	"slices"
	"sort"
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"
)

func TestDistinctLexicalCandidateAnalyzersPreserveTokenStreams(t *testing.T) {
	text := "meteorology"
	analyzers := queryAnalyzers(text)
	selected := distinctLexicalCandidateAnalyzers(text, analyzers)
	if len(selected) >= len(analyzers) {
		t.Fatalf("selected analyzers = %v, source = %v", selected, analyzers)
	}
	if got, want := lexicalCandidateAnalyzerEvidence(text, selected),
		lexicalCandidateAnalyzerEvidence(text, analyzers); !slices.Equal(got, want) {
		t.Fatalf("selected evidence = %v, want %v", got, want)
	}
}

func TestDistinctLexicalCandidateAnalyzersKeepDistinctStreams(t *testing.T) {
	text := "машины"
	analyzers := []string{"ru", standardTextAnalyzer}
	if got := distinctLexicalCandidateAnalyzers(text, analyzers); !slices.Equal(got, analyzers) {
		t.Fatalf("selected analyzers = %v, want %v", got, analyzers)
	}
}

func TestDistinctLexicalCandidateAnalyzersKeepDictionaryBranches(t *testing.T) {
	text := "搜索引擎"
	analyzers := []string{cjkChineseTextAnalyzer, cjkJapaneseTextAnalyzer}
	if got := distinctLexicalCandidateAnalyzers(text, analyzers); !slices.Equal(got, analyzers) {
		t.Fatalf("selected analyzers = %v, want %v", got, analyzers)
	}
}

func TestDistinctLexicalCandidateAnalyzersKeepUnavailableAnalyzers(t *testing.T) {
	original := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = original })
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	analyzers := []string{"missing-one", "missing-two"}
	if got := distinctLexicalCandidateAnalyzers("term", analyzers); !slices.Equal(got, analyzers) {
		t.Fatalf("selected analyzers = %v, want %v", got, analyzers)
	}
}

func TestDistinctLexicalCandidateAnalyzersKeepLegacyAnalyzer(t *testing.T) {
	analyzers := []string{""}
	if got := distinctLexicalCandidateAnalyzers("term", analyzers); !slices.Equal(got, analyzers) {
		t.Fatalf("selected analyzers = %v, want %v", got, analyzers)
	}
}

func lexicalCandidateAnalyzerEvidence(text string, analyzers []string) []string {
	evidence := make([]string, 0, len(analyzers))
	for _, analyzer := range analyzers {
		if cjkDictionaryQueryTerm(analyzer, text) {
			evidence = append(evidence, "dictionary:"+analyzer)

			continue
		}
		tokenStream, available := analyzerTermsSignature(
			cjkQueryAnalyzer(analyzer),
			[]string{text},
		)
		if !available {
			evidence = append(evidence, "unavailable:"+analyzer)

			continue
		}
		evidence = append(evidence, "tokens:"+tokenStream)
	}
	sort.Strings(evidence)

	return slices.Compact(evidence)
}
