package searchindex

import (
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/search"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

// explanationMentions reports whether the scoring explanation tree carries the
// marker anywhere — used to tell the BM25 scorer ("as per bm25 model ... k1=")
// apart from the default TF-IDF scorer ("as per tf-idf model").
func explanationMentions(expl *search.Explanation, marker string) bool {
	if expl == nil {
		return false
	}
	if strings.Contains(expl.Message, marker) {
		return true
	}
	for _, child := range expl.Children {
		if explanationMentions(child, marker) {
			return true
		}
	}

	return false
}

func explainQuery(t *testing.T, index bleve.Index) *search.Explanation {
	t.Helper()
	query := bleve.NewMatchQuery("черногория")
	query.SetField("body")
	// Pin the analyzer: an unset one is resolved by AnalyzerNameForPath, which
	// ranges over the per-language TypeMapping map, and Go randomizes map
	// iteration, so the query text was intermittently stemmed by the "ru"
	// analyzer ("черногор") and missed the surface term the default mapping
	// indexed ("черногория") — a ~1-in-N "no hits" flake (TESTFLAKE-01).
	// standard_text is the surface-form analyzer production always ORs in.
	query.Analyzer = standardTextAnalyzer
	request := bleve.NewSearchRequest(query)
	request.Explain = true
	result, err := index.Search(request)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Hits) == 0 {
		t.Fatal("no hits")
	}

	return result.Hits[0].Expl
}

func TestSearchIndexMappingUsesBM25(t *testing.T) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	if indexMapping.ScoringModel != bleveindex.BM25Scoring {
		t.Fatalf("scoring model = %q, want bm25", indexMapping.ScoringModel)
	}
}

func TestScorchShardScoresWithBM25(t *testing.T) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	index, err := bleve.NewUsing("", indexMapping, scorch.Name, scorch.Name, nil)
	if err != nil {
		t.Fatalf("open scorch: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
	if err := index.Index(
		"a",
		bleveDocument{Title: "Черногория", Body: "Черногория страна балканы"},
	); err != nil {
		t.Fatalf("index: %v", err)
	}
	if !explanationMentions(explainQuery(t, index), "k1=") {
		t.Fatal("scorch shard built from the mapping is not scoring with BM25")
	}
}

func TestEnableBM25ScoringFlipsExistingIndex(t *testing.T) {
	// A scorch index persisted before BM25 was adopted keeps the default
	// TF-IDF scoring; enableBM25Scoring must switch it with no reindex.
	legacyMapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	legacyMapping.ScoringModel = ""

	index, err := bleve.NewUsing("", legacyMapping, scorch.Name, scorch.Name, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
	if err := index.Index(
		"d1",
		bleveDocument{Title: "Черногория", Body: "черногория страна"},
	); err != nil {
		t.Fatalf("index doc: %v", err)
	}

	if explanationMentions(explainQuery(t, index), "k1=") {
		t.Fatal("legacy index should still score with TF-IDF")
	}
	enableBM25Scoring(index)
	if !explanationMentions(explainQuery(t, index), "k1=") {
		t.Fatal("enableBM25Scoring did not switch the index to BM25")
	}
}
