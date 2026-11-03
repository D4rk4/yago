package searchindex

import (
	"errors"
	"math"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

var errFieldScoreLeafRead = errors.New("field score leaf read failed")

type failingFieldScoreLeafSearcher struct {
	search.Searcher
}

func (failingFieldScoreLeafSearcher) Next(*search.SearchContext) (*search.DocumentMatch, error) {
	return nil, errFieldScoreLeafRead
}

func (failingFieldScoreLeafSearcher) Advance(
	*search.SearchContext,
	bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	return nil, errFieldScoreLeafRead
}

func TestFieldScoreSearchMatchesFullExplanation(t *testing.T) {
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: []documentstore.Document{
			{
				NormalizedURL: "https://a.example/linux-kernel",
				Title:         "linux kernel guide",
				ExtractedText: "the linux kernel scheduler manages processes",
			},
			{
				NormalizedURL: "https://b.example/notes",
				Title:         "operating system notes",
				ExtractedText: "a kernel is the core of linux and other systems",
			},
		}},
	)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	request := SearchRequest{
		Query:      "linux kernel",
		Terms:      []string{"linux", "kernel"},
		MaxResults: 5,
	}
	explained := request
	explained.Explain = true
	scored := request
	scored.IncludeFieldScores = true

	full, err := index.Search(t.Context(), explained)
	if err != nil {
		t.Fatalf("explained search: %v", err)
	}
	cheap, err := index.Search(t.Context(), scored)
	if err != nil {
		t.Fatalf("field score search: %v", err)
	}
	if len(full.Results) != 2 || len(cheap.Results) != len(full.Results) {
		t.Fatalf("results = %d explained, %d scored, want 2", len(full.Results), len(cheap.Results))
	}
	titleScored := false
	for position, want := range full.Results {
		assertFieldScoresMatchExplanation(t, cheap.Results[position], want)
		titleScored = titleScored || want.FieldScores["title"] > 0
	}
	if !titleScored {
		t.Fatal("no result carries a title score")
	}
}

func assertFieldScoresMatchExplanation(t *testing.T, got SearchResult, want SearchResult) {
	t.Helper()
	if want.Explanation == "" || got.Explanation != "" {
		t.Fatalf("explanation explained=%q scored=%q", want.Explanation, got.Explanation)
	}
	if got.URL != want.URL || got.Score != want.Score {
		t.Fatalf("result = %s %v, want %s %v", got.URL, got.Score, want.URL, want.Score)
	}
	if want.FieldScores["body"] <= 0 {
		t.Fatalf("explained field scores = %v, want body", want.FieldScores)
	}
	if len(got.FieldScores) != len(want.FieldScores) {
		t.Fatalf("field scores = %v, want %v", got.FieldScores, want.FieldScores)
	}
	for field, score := range want.FieldScores {
		if math.Abs(got.FieldScores[field]-score) > 1e-9 {
			t.Fatalf("%s score = %v, want %v", field, got.FieldScores[field], score)
		}
	}
}

func TestFieldScoreLeavesExplainOnlyFieldScoreSearches(t *testing.T) {
	index := fieldScoreLeafIndex(t)
	term := bleve.NewTermQuery("linux")
	term.SetField("body")
	term.SetBoost(3)
	for _, leaf := range []struct {
		name         string
		query        blevequery.Query
		contribution string
	}{
		{name: "match", query: fieldMatch("body", "Kernel", 2), contribution: "weight(body:kernel^2.000000 in "},
		{name: "term", query: bleveFieldScoreTermQuery{term}, contribution: "weight(body:linux^3.000000 in "},
	} {
		t.Run(leaf.name, func(t *testing.T) {
			cheap := searchFieldScoreLeaf(
				t,
				index,
				leaf.query,
				SearchRequest{IncludeFieldScores: true},
				true,
			)
			if cheap.Expl == nil || cheap.Expl.Message != leaf.contribution ||
				cheap.Expl.Value != cheap.Score || len(cheap.Expl.Children) != 0 {
				t.Fatalf("field score explanation = %#v, score %v", cheap.Expl, cheap.Score)
			}
			full := searchFieldScoreLeaf(
				t, index, leaf.query, SearchRequest{IncludeFieldScores: true, Explain: true}, true,
			)
			if full.Expl == nil || len(full.Expl.Children) == 0 || full.Score != cheap.Score {
				t.Fatalf("diagnostic explanation = %#v score %v, want the full tree, score %v",
					full.Expl, full.Score, cheap.Score)
			}
			unexplained := searchFieldScoreLeaf(
				t,
				index,
				leaf.query,
				SearchRequest{IncludeFieldScores: true},
				false,
			)
			if unexplained.Expl != nil {
				t.Fatalf("unexplained search carries %#v", unexplained.Expl)
			}
		})
	}
}

func TestFieldScoreLeafReportsOpenFailure(t *testing.T) {
	query := fieldMatch("body", "kernel", 1)
	query.Analyzer = "missing-analyzer"
	request := bleve.NewSearchRequest(query)
	request.Explain = true

	_, err := fieldScoreLeafIndex(t).SearchInContext(
		bleveFieldScoreSearchContext(t.Context(), SearchRequest{IncludeFieldScores: true}),
		request,
	)
	if err == nil {
		t.Fatal("search with a missing analyzer succeeded")
	}
}

func TestFieldScoreSearcherReportsReadFailure(t *testing.T) {
	leaf := bleveFieldScoreSearcher{
		Searcher:     failingFieldScoreLeafSearcher{},
		contribution: "unused",
	}
	if match, err := leaf.Next(nil); match != nil || !errors.Is(err, errFieldScoreLeafRead) {
		t.Fatalf("Next = %v, %v, want the read failure", match, err)
	}
	if match, err := leaf.Advance(
		nil,
		nil,
	); match != nil ||
		!errors.Is(err, errFieldScoreLeafRead) {
		t.Fatalf("Advance = %v, %v, want the read failure", match, err)
	}
}

func fieldScoreLeafIndex(t *testing.T) bleve.Index {
	t.Helper()
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("NewMemOnly: %v", err)
	}
	if err := index.Index("doc", map[string]string{"body": "linux kernel"}); err != nil {
		t.Fatalf("index: %v", err)
	}

	return index
}

func searchFieldScoreLeaf(
	t *testing.T,
	index bleve.Index,
	query blevequery.Query,
	req SearchRequest,
	explain bool,
) *search.DocumentMatch {
	t.Helper()
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Explain = explain
	result, err := index.SearchInContext(
		bleveFieldScoreSearchContext(t.Context(), req),
		searchRequest,
	)
	if err != nil || len(result.Hits) != 1 {
		t.Fatalf("search = %v, %v", result, err)
	}

	return result.Hits[0]
}
