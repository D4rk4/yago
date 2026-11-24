package searchindex

import (
	"context"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

func TestFuzzyCandidateFailureIsNotAnEmptyResult(t *testing.T) {
	for _, stage := range []string{"open", "close", "combine", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			failure := errors.New("candidate unavailable")
			id := bleveindex.NewIndexInternalID(nil, 1)
			first := newBleveLexicalCandidateSearcherProbe(id)
			last := newBleveLexicalCandidateSearcherProbe()
			firstQuery := &bleveLexicalCandidateQueryProbe{searcher: first}
			lastQuery := &bleveLexicalCandidateQueryProbe{searcher: last}
			switch stage {
			case "open":
				lastQuery.err, lastQuery.searcher = failure, nil
			case "close":
				first.closeErr = failure
			case "combine":
				last.identities = []bleveindex.IndexInternalID{id}
				firstQuery.searcher = failedLexicalScoringOptimization{first, failure}
				lastQuery.searcher = failedLexicalScoringOptimization{last, failure}
			case "cancel":
				cancel(failure)
			}
			opened, err := (fuzzyCandidateRequirements{firstQuery, lastQuery}).Searcher(
				ctx,
				nil,
				mapping.NewIndexMapping(),
				search.SearcherOptions{},
			)
			if opened != nil || !errors.Is(err, failure) || !first.closed ||
				last.closed != (stage != "open") {
				t.Fatalf(
					"searcher=%T failure=%v closed=%t/%t",
					opened,
					err,
					first.closed,
					last.closed,
				)
			}
		})
	}
}
