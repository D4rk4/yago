package searchindex

import (
	"fmt"
	"testing"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type bleveScoredHit struct {
	ID    string
	Score float64
}

func TestBleveSearchSnapshotKeepsProductionQueryScores(t *testing.T) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatal(err)
	}
	index := newBleveTermPostingsShard(t, indexMapping)
	indexBleveScoringFixture(t, index)
	multilingual := supportsMultilingualAnalyzers(index)
	analyzerScope := supportsAnalyzerScope(index)
	for _, request := range []SearchRequest{
		{Query: "river stone"},
		{Query: "river stone bridge garden lantern", Relaxed: true},
		{Query: "stone bridge river", Phrases: []string{"stone bridge"}},
		{Query: "rivr stone", Fuzzy: true},
	} {
		t.Run(request.Query, func(t *testing.T) {
			query := bleveSearchQuery(request, multilingual, analyzerScope)
			want := bleveScoredHits(t, index, query)
			if len(want) < 2 {
				t.Fatalf("plain search hits = %+v, want several", want)
			}
			got := bleveScoredHits(t, index, withBleveSearchDeadline(request, query))
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("search hits = %+v\nwant %+v", got, want)
			}
		})
	}
}

func indexBleveScoringFixture(t *testing.T, index bleve.Index) {
	t.Helper()
	indexBleveTermPostingsBatch(t, index, bleveScoringDocuments(t, map[string]string{
		"https://a.example/1": "river stone bridge over the quiet river",
		"https://a.example/2": "stone garden with a lantern near the bridge",
		"https://a.example/3": "the river bends past an old stone mill",
		"https://a.example/4": "lantern festival on the river bridge",
		"https://a.example/5": "garden stone paths and river views",
	}))
	indexBleveTermPostingsBatch(t, index, bleveScoringDocuments(t, map[string]string{
		"https://b.example/1": "stone bridge river crossing at dawn",
		"https://b.example/2": "a garden lantern lights the stone wall",
		"https://b.example/3": "river stone river stone river",
		"https://b.example/4": "bridge repairs close the river road",
	}))
	forceSingleScorchSegment(t, index)
	deleteBleveTermPostingsDocuments(t, index, "https://a.example/3", "https://b.example/2")
	indexBleveTermPostingsBatch(t, index, bleveScoringDocuments(t, map[string]string{
		"https://a.example/1": "stone bridge above a narrow river gorge",
		"https://c.example/1": "garden bridge and lantern by the river",
	}))
	indexBleveTermPostingsBatch(t, index, bleveScoringDocuments(t, map[string]string{
		"https://c.example/2": "river stone collection in the garden",
		"https://c.example/3": "the stone bridge spans the river",
	}))
}

func bleveScoringDocuments(t *testing.T, bodies map[string]string) map[string]any {
	t.Helper()
	documents := make(map[string]any, len(bodies))
	for url, body := range bodies {
		indexed, err := bleveDocumentFromStore(documentstore.Document{
			NormalizedURL: url,
			Title:         body,
			Headings:      []string{body},
			ExtractedText: body + " " + body,
			Language:      "en",
		})
		if err != nil {
			t.Fatal(err)
		}
		documents[url] = indexed
	}

	return documents
}

func bleveScoredHits(t *testing.T, index bleve.Index, query blevequery.Query) []bleveScoredHit {
	t.Helper()
	request := bleve.NewSearchRequest(query)
	request.Size = 100
	result, err := index.SearchInContext(t.Context(), request)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	hits := make([]bleveScoredHit, 0, len(result.Hits))
	for _, hit := range result.Hits {
		hits = append(hits, bleveScoredHit{ID: hit.ID, Score: hit.Score})
	}

	return hits
}
