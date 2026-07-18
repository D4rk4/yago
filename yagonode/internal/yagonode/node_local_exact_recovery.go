package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type localExactRecoverySearcher struct {
	primary searchcore.Searcher
	local   searchcore.Searcher
}

func withLocalExactRecovery(
	primary searchcore.Searcher,
	local searchcore.Searcher,
) searchcore.Searcher {
	return localExactRecoverySearcher{primary: primary, local: local}
}

func (s localExactRecoverySearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	response, err := s.primary.Search(ctx, req)
	if err != nil {
		return response, fmt.Errorf("local exact primary: %w", err)
	}
	if len(response.Results) > 0 || !hasExactStageFailure(response) {
		return response, nil
	}
	recovered, recoveryErr := withLocalExactRecoveryBudgetForFailure(
		s.local,
		response,
	).Search(ctx, req)
	if recoveryErr != nil {
		return response, fmt.Errorf("local exact recovery: %w", recoveryErr)
	}
	if len(recovered.Results) == 0 {
		response.PartialFailures = append(
			response.PartialFailures,
			recovered.PartialFailures...,
		)

		return response, nil
	}
	recovered.Request = req
	recovered.TotalResults = max(recovered.TotalResults, len(recovered.Results))
	recovered.PartialFailures = append(
		append([]searchcore.PartialFailure(nil), response.PartialFailures...),
		recovered.PartialFailures...,
	)

	return recovered, nil
}

func hasExactStageFailure(response searchcore.Response) bool {
	for _, failure := range response.PartialFailures {
		if failure.Source == webFallbackExactStageFailureSource {
			return true
		}
	}

	return false
}
