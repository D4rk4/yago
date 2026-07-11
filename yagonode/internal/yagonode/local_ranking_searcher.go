package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
)

func newLocalRankingSearcher(
	index searchindex.SearchIndex,
	weights func() searchindex.RankingWeights,
	hostRank func() hostrank.AuthorityTable,
) searchcore.Searcher {
	return searchlocal.NewSearcherWithRanking(index, weights, hostRank)
}
