package searchindex

import (
	"fmt"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestBleveLexicalMissSkipsScoredAnalyzerScopes(t *testing.T) {
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: lexicalCandidateDocuments()},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.index.Close() })
	advanced, err := index.index.Advanced()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := advanced.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	probe := &bleveLexicalCandidatePageProbe{result: &bleve.SearchResult{
		Status: &bleve.SearchStatus{Total: 1, Successful: 1},
	}}
	disk := &BleveDiskIndex{alias: probe, analyzerScope: true, multilingual: true}
	request := SearchRequest{Query: "absentlexeme", MaxResults: 1}
	if _, err := disk.searchLexicalCandidateHitPage(t.Context(), request, 1); err != nil {
		t.Fatal(err)
	}
	scopeReads := 0
	reads := &bleveSearchDeadlineIndexReaderProbe{IndexReader: reader}
	reads.hook = func() {
		if reads.field == documentAnalyzerField {
			scopeReads++
		}
		reads.opened, reads.err = reader.TermFieldReader(
			reads.context, []byte(reads.term), reads.field,
			reads.includeFrequency, reads.includeNorm, reads.includeTermVectors,
		)
	}
	opened, err := probe.requests[0].Query.Searcher(
		t.Context(), reads, index.index.Mapping(), search.SearcherOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = opened.Close() }()
	if opened.Count() != 0 || scopeReads != 0 {
		t.Fatalf("miss candidates=%d scored analyzer reads=%d", opened.Count(), scopeReads)
	}
}

func BenchmarkBleveLexicalMiss(b *testing.B) {
	documents := make([]documentstore.Document, 1024)
	for ordinal := range documents {
		documents[ordinal] = documentstore.Document{
			NormalizedURL: fmt.Sprintf("https://example.org/page/%d", ordinal),
			Title:         "Regionalterm guide", ExtractedText: "Regionalterm reference material",
			Language: "en",
		}
	}
	index, err := NewBleveDiskIndex(
		b.Context(), b.TempDir(), newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = index.Close() })
	request := SearchRequest{
		Query:         "regionalterm absentlexeme",
		MaxResults:    10,
		CandidateOnly: true,
	}
	b.ResetTimer()
	for b.Loop() {
		result, err := index.Search(b.Context(), request)
		if err != nil || result.Total != 0 || len(result.Results) != 0 {
			b.Fatalf("result=%+v error=%v", result, err)
		}
	}
}
