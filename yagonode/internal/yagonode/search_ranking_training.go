package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type rankingTrainingSearcher struct {
	searcher searchcore.Searcher
}

func (s rankingTrainingSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	req.RankingFeatures = true
	response, err := s.searcher.Search(ctx, req)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("ranking training search: %w", err)
	}

	return response, nil
}
