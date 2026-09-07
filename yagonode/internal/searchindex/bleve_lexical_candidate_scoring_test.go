package searchindex

import (
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type failedLexicalScoringOptimization struct {
	*bleveLexicalCandidateSearcherProbe
	err error
}

func (probe failedLexicalScoringOptimization) Optimize(
	string,
	bleveindex.OptimizableContext,
) (bleveindex.OptimizableContext, error) {
	return nil, probe.err
}

func TestBleveLexicalScoringClosesCandidatesOnFailure(t *testing.T) {
	want := errors.New("scoring unavailable")
	for _, stage := range []string{"candidates", "scoring", "conjunction"} {
		t.Run(stage, func(t *testing.T) {
			first := bleveindex.NewIndexInternalID(nil, 1)
			second := bleveindex.NewIndexInternalID(nil, 2)
			candidates := newBleveLexicalCandidateSearcherProbe(first, second)
			ranked := newBleveLexicalCandidateSearcherProbe(first)
			candidateQuery := &bleveLexicalCandidateQueryProbe{searcher: candidates}
			rankingQuery := &bleveLexicalCandidateQueryProbe{searcher: ranked}
			switch stage {
			case "candidates":
				candidateQuery.searcher, candidateQuery.err = nil, want
			case "scoring":
				rankingQuery.searcher, rankingQuery.err = nil, want
			case "conjunction":
				rankingQuery.searcher = failedLexicalScoringOptimization{ranked, want}
			}
			query := bleveLexicalCandidateScoringQuery{
				bleve.NewConjunctionQuery(rankingQuery, candidateQuery),
			}
			opened, err := query.Searcher(
				t.Context(),
				nil,
				mapping.NewIndexMapping(),
				search.SearcherOptions{},
			)
			if opened != nil || !errors.Is(err, want) {
				t.Fatalf("opened=%T error=%v", opened, err)
			}
			if candidates.closed != (stage != "candidates") ||
				ranked.closed != (stage == "conjunction") {
				t.Fatalf("closed candidates=%t scoring=%t", candidates.closed, ranked.closed)
			}
			if stage == "candidates" && rankingQuery.calls != 0 {
				t.Fatal("scoring started after candidate failure")
			}
		})
	}
}
