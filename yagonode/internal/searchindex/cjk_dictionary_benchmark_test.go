package searchindex

import (
	"fmt"
	"testing"

	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

var (
	cjkBenchmarkMapping *mapping.IndexMappingImpl
	cjkBenchmarkTokens  analysis.TokenStream
	cjkBenchmarkResults SearchResultSet
)

func BenchmarkCJKMappingConstruction(b *testing.B) {
	for range b.N {
		indexMapping, err := newSearchIndexMapping()
		if err != nil {
			b.Fatal(err)
		}
		cjkBenchmarkMapping = indexMapping
	}
}

func BenchmarkCJKChineseFirstDocumentAnalysis(b *testing.B) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		b.Fatal(err)
	}
	analyzer := indexMapping.AnalyzerNamed(cjkChineseTextAnalyzer)
	b.ResetTimer()
	for range b.N {
		cjkBenchmarkTokens = analyzer.Analyze([]byte("搜尋軟體搜索引擎程序设计语言"))
	}
}

func BenchmarkCJKJapaneseFirstDocumentAnalysis(b *testing.B) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		b.Fatal(err)
	}
	analyzer := indexMapping.AnalyzerNamed(cjkJapaneseTextAnalyzer)
	b.ResetTimer()
	for range b.N {
		cjkBenchmarkTokens = analyzer.Analyze([]byte("プログラミング言語と検索エンジン"))
	}
}

func BenchmarkCJKChineseQueryAnalysis(b *testing.B) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		b.Fatal(err)
	}
	analyzer := indexMapping.AnalyzerNamed(cjkChineseQueryAnalyzer)
	cjkBenchmarkTokens = analyzer.Analyze([]byte("搜索引擎程序设计"))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		cjkBenchmarkTokens = analyzer.Analyze([]byte("搜索引擎程序设计"))
	}
}

func BenchmarkCJKChineseDocumentAnalysis(b *testing.B) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		b.Fatal(err)
	}
	analyzer := indexMapping.AnalyzerNamed(cjkChineseTextAnalyzer)
	cjkBenchmarkTokens = analyzer.Analyze([]byte("搜尋軟體搜索引擎程序设计语言"))
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		cjkBenchmarkTokens = analyzer.Analyze([]byte("搜尋軟體搜索引擎程序设计语言"))
	}
}

func BenchmarkCJKChineseIndexDocument(b *testing.B) {
	index, err := NewBleveMemoryIndex(b.Context(), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := index.index.Close(); err != nil {
			b.Errorf("close benchmark index: %v", err)
		}
	})
	b.ResetTimer()
	b.ReportAllocs()
	for number := range b.N {
		if err := index.Index(b.Context(), documentstore.Document{
			NormalizedURL: fmt.Sprintf("https://benchmark.example/%d", number),
			Title:         "搜索引擎程序设计",
			ExtractedText: "搜尋軟體搜索引擎程序设计语言",
			Language:      "zh-Hans",
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCJKChineseSearch(b *testing.B) {
	index, err := NewBleveMemoryIndex(b.Context(), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := index.index.Close(); err != nil {
			b.Errorf("close benchmark index: %v", err)
		}
	})
	for number := range 20 {
		if err := index.Index(b.Context(), documentstore.Document{
			NormalizedURL: fmt.Sprintf("https://benchmark.example/%d", number),
			Title:         "搜索引擎程序设计",
			ExtractedText: "搜尋軟體搜索引擎程序设计语言",
			Language:      "zh-Hans",
		}); err != nil {
			b.Fatal(err)
		}
	}
	request := SearchRequest{
		Query:            "搜索引擎 程序设计",
		Terms:            []string{"搜索引擎", "程序设计"},
		MaxResults:       10,
		IncludePositions: true,
	}
	cjkBenchmarkResults, err = index.Search(b.Context(), request)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		cjkBenchmarkResults, err = index.Search(b.Context(), request)
		if err != nil {
			b.Fatal(err)
		}
	}
}
