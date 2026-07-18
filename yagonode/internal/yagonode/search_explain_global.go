package yagonode

import (
	"context"
	"fmt"
	"math"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type searchExplainOutcome struct {
	ranking         learnedrank.Outcome
	partialFailures []searchcore.PartialFailure
}

func (e *searchExplainEndpoint) withGlobal(global searchcore.Searcher) *searchExplainEndpoint {
	e.global = global

	return e
}

func (e searchExplainEndpoint) rankingOutcome(
	ctx context.Context,
	query string,
	weights searchindex.RankingWeights,
	scope searchcore.Source,
) (searchExplainOutcome, error) {
	explain := func(executionContext context.Context) (searchExplainOutcome, error) {
		return e.unboundedRankingOutcome(
			executionContext,
			query,
			weights,
			scope,
		)
	}

	return e.execution.execute(ctx, explain)
}

func (e searchExplainEndpoint) unboundedRankingOutcome(
	ctx context.Context,
	query string,
	weights searchindex.RankingWeights,
	scope searchcore.Source,
) (searchExplainOutcome, error) {
	if scope == searchcore.SourceLocal {
		if e.global == nil {
			return e.localRankingOutcome(ctx, query, weights)
		}
		local := e.global
		if weighted, ok := local.(interface {
			localExplanationWithWeights(searchindex.RankingWeights) searchcore.Searcher
		}); ok {
			local = weighted.localExplanationWithWeights(weights)
		}

		return e.searcherRankingOutcome(ctx, query, scope, local)
	}

	return e.globalRankingOutcome(ctx, query)
}

func (e searchExplainEndpoint) globalRankingOutcome(
	ctx context.Context,
	query string,
) (searchExplainOutcome, error) {
	return e.searcherRankingOutcome(ctx, query, searchcore.SourceGlobal, e.global)
}

func (e searchExplainEndpoint) searcherRankingOutcome(
	ctx context.Context,
	query string,
	scope searchcore.Source,
	searcher searchcore.Searcher,
) (searchExplainOutcome, error) {
	servingRequest := searchcore.RequestWithParsedQuery(searchcore.Request{
		Query:   query,
		Source:  scope,
		Limit:   searchExplainMaxResults,
		Explain: true,
	})
	candidateRequest := servingRequest
	if e.ranker != nil {
		if _, active := e.ranker.ActiveSnapshot(); active {
			candidateRequest.Limit = max(
				candidateRequest.Limit,
				e.ranker.CandidateWindow(),
			)
		}
	}
	response, err := searcher.Search(ctx, candidateRequest)
	if err != nil {
		return searchExplainOutcome{}, fmt.Errorf("search failed: %w", err)
	}
	outcome := learnedrank.Outcome{Results: response.Results}
	if e.ranker != nil {
		outcome, err = e.ranker.Rerank(servingRequest, response.Results)
		if err != nil {
			return searchExplainOutcome{}, fmt.Errorf("learned ranking failed: %w", err)
		}
	}
	outcome.Results = searchcore.DiversifyResults(outcome.Results, servingRequest)
	searchcore.OrderByDateWhenRequested(outcome.Results, servingRequest)
	outcome.Results = outcome.Results[:min(len(outcome.Results), searchExplainMaxResults)]

	return searchExplainOutcome{
		ranking: outcome, partialFailures: response.PartialFailures,
	}, nil
}

func searchExplainSource(result searchcore.Result) string {
	if result.FromPeer() {
		return "peer"
	}
	if result.FromWeb() {
		return "web"
	}

	return "local"
}

func searchExplainRetrievalScore(
	result searchcore.Result,
	explanation *learnedrank.ResultExplanation,
) float64 {
	if explanation != nil {
		return explanation.OriginalScore
	}

	return result.Score
}

func searchExplainFusionContributions(
	result searchcore.Result,
	explanation *learnedrank.ResultExplanation,
) []searchExplainFusion {
	mappings := []struct {
		signal searchcore.RankingSignal
		branch string
	}{
		{searchcore.SignalStrictRank, "strict"},
		{searchcore.SignalRelaxedRank, "relaxed"},
		{searchcore.SignalFeedbackRank, "feedback"},
		{searchcore.SignalLocalRank, "local"},
		{searchcore.SignalRemoteRank, "peer"},
		{searchcore.SignalWebRank, "web"},
	}
	contributions := make([]searchExplainFusion, 0, len(mappings)+1)
	for _, mapping := range mappings {
		value, known := result.Evidence.Value(mapping.signal)
		rank := int(math.Round(value))
		if !known || rank <= 0 || math.Abs(value-float64(rank)) > 1e-9 {
			continue
		}
		contributions = append(contributions, searchExplainFusion{
			Branch: mapping.branch, Rank: rank,
			Contribution: searchcore.ReciprocalRankContribution(rank),
		})
	}
	_, webRankKnown := result.Evidence.Value(searchcore.SignalWebRank)
	if result.Source == searchcore.SourceWeb && !webRankKnown {
		retrievalScore := searchExplainRetrievalScore(result, explanation)
		if rank, exact := searchcore.RankFromReciprocalContribution(retrievalScore); exact {
			contributions = append(contributions, searchExplainFusion{
				Branch: "web", Rank: rank, Contribution: retrievalScore,
			})
		}
	}

	return contributions
}
