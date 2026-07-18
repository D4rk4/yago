package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type publicSearchExplanationSource struct {
	serving  searchcore.Searcher
	assembly publicSearchAssembly
}

func newPublicSearchExplanationSource(
	local searchcore.Searcher,
	remote searchcore.Searcher,
	assembly publicSearchAssembly,
) publicSearchExplanationSource {
	return publicSearchExplanationSource{
		serving:  assembleExplanationEvidenceSearcher(local, remote, assembly),
		assembly: assembly,
	}
}

func (s publicSearchExplanationSource) Search(
	ctx context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	response, err := s.serving.Search(ctx, request)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("search explanation retrieval failed: %w", err)
	}

	return response, nil
}

func (s publicSearchExplanationSource) localExplanationWithWeights(
	weights searchindex.RankingWeights,
) searchcore.Searcher {
	if s.assembly.storage.searchIndex == nil {
		return s.serving
	}
	provider := func() searchindex.RankingWeights { return weights }
	assembly := s.assembly
	assembly.rankingWeights = provider
	local := newLocalRankingSearcher(
		assembly.storage.searchIndex,
		provider,
		assembly.hostRank,
	)

	return assembleExplanationEvidenceSearcher(local, nil, assembly)
}

func assembleExplanationEvidenceSearcher(
	local searchcore.Searcher,
	remote searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	retrieval := assemblePublicRetrievalSearcher(local, remote, assembly)
	explanation := assembleRankingEvidenceStages(retrieval, assembly)
	explanation = withEffectiveWebFallbackRequest(explanation, assembly.webFallback)

	return withParsedQuery(explanation)
}
