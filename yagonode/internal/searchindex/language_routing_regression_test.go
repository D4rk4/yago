package searchindex

import (
	"slices"
	"strconv"
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

var arabicAnalyzerFanoutBenchmarkTotal uint64

func TestArabicScriptQueryIncludesCentralKurdishAnalyzer(t *testing.T) {
	want := []string{"ar", "fa", "ckb", standardTextAnalyzer}
	if got := queryAnalyzers("گەڕان بە زمانی کوردی"); !slices.Equal(got, want) {
		t.Fatalf("Arabic-script analyzers = %v, want %v", got, want)
	}
}

func TestReliableKurdishAnalyzerFollowsScript(t *testing.T) {
	if got := reliableLanguageAnalyzer("ku", "گەڕان بە زمانی کوردی"); got != "ckb" {
		t.Fatalf("Arabic-script Kurdish analyzer = %q", got)
	}
	if got := reliableLanguageAnalyzer("ku", "geran bi zimanê kurdî"); got != standardTextAnalyzer {
		t.Fatalf("Latin-script Kurdish analyzer = %q", got)
	}
	if got := reliableLanguageAnalyzer("fa", "جستجوی فارسی"); got != "fa" {
		t.Fatalf("Persian analyzer = %q", got)
	}
}

func TestExplicitKurdishDocumentAnalyzerFollowsScript(t *testing.T) {
	if got := detectDocumentAnalyzer("گەڕان بە زمانی کوردی", "ku-IQ"); got != "ckb" {
		t.Fatalf("Sorani document analyzer = %q", got)
	}
	if got := detectDocumentAnalyzer("geran bi zimanê kurdî", "ku"); got != standardTextAnalyzer {
		t.Fatalf("Kurmanji document analyzer = %q", got)
	}
}

func TestExplicitSoraniDocumentRetrievesStemVariant(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	t.Cleanup(func() {
		if err := index.index.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := index.Index(t.Context(), documentstore.Document{
		NormalizedURL: "https://example.test/sorani",
		ExtractedText: "پیاوە",
		Language:      "ku",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "پیاو", Terms: []string{"پیاو"}, MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Analyzer != "ckb" {
		t.Fatalf("Sorani stem retrieval = %#v", result.Results)
	}
}

func BenchmarkArabicAnalyzerFanoutSearch(b *testing.B) {
	index := arabicAnalyzerFanoutBenchmarkIndex(b)
	req := SearchRequest{
		Query: "أحمد يوسف", Terms: []string{"أحمد", "يوسف"},
		MaxResults: 10,
	}
	for _, benchmark := range []struct {
		name      string
		analyzers []string
	}{
		{name: "before-ckb", analyzers: []string{"ar", "fa", standardTextAnalyzer}},
		{name: "with-ckb", analyzers: []string{"ar", "fa", "ckb", standardTextAnalyzer}},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				request := bleve.NewSearchRequest(requiredTermsQuery(
					req,
					benchmark.analyzers,
					req.Weights.orDefault(),
					true,
				))
				request.Size = req.MaxResults
				result, err := index.index.SearchInContext(b.Context(), request)
				if err != nil {
					b.Fatal(err)
				}
				if result.Total != 1 {
					b.Fatalf("result total = %d", result.Total)
				}
				arabicAnalyzerFanoutBenchmarkTotal = result.Total
			}
		})
	}
}

func arabicAnalyzerFanoutBenchmarkIndex(b *testing.B) *BleveMemoryIndex {
	b.Helper()
	index, err := NewBleveMemoryIndex(b.Context(), nil)
	if err != nil {
		b.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	b.Cleanup(func() {
		if err := index.index.Close(); err != nil {
			b.Errorf("Close: %v", err)
		}
	})
	for ordinal := range 64 {
		text := "محتوى تقني عام للبحث"
		if ordinal == 0 {
			text = "أحمد يوسف"
		}
		if err := index.Index(b.Context(), documentstore.Document{
			NormalizedURL: "https://example.test/arabic/" + strconv.Itoa(ordinal),
			ExtractedText: text,
			Language:      "ar",
		}); err != nil {
			b.Fatalf("Index(%d): %v", ordinal, err)
		}
	}

	return index
}
