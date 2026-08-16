package searchindex

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/index/scorch/mergeplan"
)

func TestBleveMergePreservesPostingChunksSeparatedByEmptyRange(t *testing.T) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatal(err)
	}
	index, err := newBleveShard(filepath.Join(t.TempDir(), "search.idx"), indexMapping)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	indexEmptyMiddlePostingSegments(t, index)
	forceSingleScorchSegment(t, index)
	requireEmptyMiddlePostingMatches(t, index)
}

func indexEmptyMiddlePostingSegments(t *testing.T, index bleve.Index) {
	t.Helper()
	for segmentNumber := range 2 {
		batch := index.NewBatch()
		for localDocument := range 2000 {
			documentNumber := segmentNumber*2000 + localDocument
			body := "ordinary fixed"
			if documentNumber < 1024 || documentNumber >= 2976 {
				body = "needle fixed"
			}
			if err := batch.Index(
				fmt.Sprintf("https://example.org/%04d", documentNumber),
				bleveDocument{Body: body, Analyzer: standardTextAnalyzer},
			); err != nil {
				t.Fatal(err)
			}
		}
		if err := index.Batch(batch); err != nil {
			t.Fatal(err)
		}
	}
}

func forceSingleScorchSegment(t *testing.T, index bleve.Index) {
	t.Helper()
	advanced, err := index.Advanced()
	if err != nil {
		t.Fatal(err)
	}
	scorchIndex, ok := advanced.(*scorch.Scorch)
	if !ok {
		t.Fatalf("advanced index type = %T", advanced)
	}
	if err := scorchIndex.ForceMerge(context.Background(), &mergeplan.MergePlanOptions{
		MaxSegmentsPerTier:   1,
		MaxSegmentSize:       5000,
		SegmentsPerMergeTask: 10,
		FloorSegmentSize:     5000,
	}); err != nil {
		t.Fatal(err)
	}
	fileSegments, ok := scorchIndex.StatsMap()["num_root_filesegments"].(uint64)
	if !ok || fileSegments != 1 {
		t.Fatalf("root file segments = %v, want 1", fileSegments)
	}
}

func requireEmptyMiddlePostingMatches(t *testing.T, index bleve.Index) {
	t.Helper()
	query := bleve.NewTermQuery("needle")
	query.SetField("body")
	result, err := index.Search(bleve.NewSearchRequestOptions(query, 1, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2048 {
		t.Fatalf("term matches = %d, want 2048", result.Total)
	}
}
