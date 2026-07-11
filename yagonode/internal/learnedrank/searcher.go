package learnedrank

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type rankingSearcher struct {
	inner  searchcore.Searcher
	ranker *Ranker
}

func NewSearcher(inner searchcore.Searcher, ranker *Ranker) searchcore.Searcher {
	if ranker == nil {
		return inner
	}

	return rankingSearcher{inner: inner, ranker: ranker}
}

func (s rankingSearcher) Search(
	ctx context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	candidateRequest := request
	if _, active := s.ranker.ActiveSnapshot(); active {
		limit := request.Limit
		if limit <= 0 {
			limit = searchcore.DefaultPublicLimit
		}
		candidateWindow := s.ranker.CandidateWindow()
		if request.Source == searchcore.SourceGlobal {
			candidateWindow *= 2
		}
		candidateRequest.Offset = 0
		candidateRequest.Limit = max(
			candidateWindow,
			request.Offset+limit,
		)
	}
	response, err := s.inner.Search(ctx, candidateRequest)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("learned ranking inner search: %w", err)
	}
	outcome, err := s.ranker.Rerank(request, response.Results)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("apply learned ranking model: %w", err)
	}
	response.Results = outcome.Results
	response.Request = request

	return response, nil
}
