package searchindex

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/blevesearch/bleve/v2"
)

const progressiveSearchReciprocalRankConstant = 60

func (b *BleveDiskIndex) SearchProgressively(
	ctx context.Context,
	req SearchRequest,
	notify func(SearchProgress),
) (SearchResultSet, error) {
	if len(b.shards) < 2 || req.WithFacets || hasPostFilters(req) {
		return b.searchProgressivelyWithoutShardWalk(ctx, req, notify)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return SearchResultSet{}, fmt.Errorf("search index closed")
	}
	if err := ctx.Err(); err != nil {
		return SearchResultSet{Incomplete: true, TotalShards: len(b.shards)},
			fmt.Errorf("progressive search context: %w", context.Cause(ctx))
	}

	order, err := randomShardOrder(len(b.shards), b.randomIndex)
	if err != nil {
		return SearchResultSet{Incomplete: true, TotalShards: len(b.shards)},
			fmt.Errorf("randomize progressive shard order: %w", err)
	}

	return b.searchProgressivelyInOrder(ctx, req, notify, order)
}

func (b *BleveDiskIndex) searchProgressivelyWithoutShardWalk(
	ctx context.Context,
	req SearchRequest,
	notify func(SearchProgress),
) (SearchResultSet, error) {
	set, err := b.Search(ctx, req)
	set.CompletedShards = len(b.shards)
	set.TotalShards = len(b.shards)
	if err == nil && set.Incomplete {
		set.SampledShards = len(b.shards)
	}
	set.Incomplete = err != nil || set.Incomplete
	if notify != nil {
		notify(SearchProgress{
			Results: set, CompletedShards: set.CompletedShards,
			SampledShards: set.SampledShards,
			TotalShards:   set.TotalShards, Complete: !set.Incomplete,
		})
	}

	return set, err
}

func (b *BleveDiskIndex) searchProgressivelyInOrder(
	ctx context.Context,
	req SearchRequest,
	notify func(SearchProgress),
	order []int,
) (SearchResultSet, error) {
	combined := SearchResultSet{TotalShards: len(b.shards)}
	completed := 0
	var searchErrors []error
	for _, shardNumber := range order {
		if err := ctx.Err(); err != nil {
			searchErrors = append(searchErrors, context.Cause(ctx))
			break
		}

		result, searchErr := b.searchProgressiveShard(ctx, req, shardNumber)
		if searchErr != nil {
			searchErrors = append(searchErrors,
				fmt.Errorf("search progressive shard %d: %w", shardNumber, searchErr))
			if ctx.Err() != nil {
				break
			}

			continue
		}

		completed++
		combined = mergeProgressiveShardResult(combined, result, req.MaxResults, completed)
		if result.Incomplete {
			combined.SampledShards++
		}
		if notify != nil {
			notify(SearchProgress{
				Results: combined, CompletedShards: completed,
				SampledShards: combined.SampledShards,
				TotalShards:   len(b.shards),
				Complete:      completed == len(b.shards) && !combined.Incomplete,
			})
		}
	}
	combined.CompletedShards = completed
	combined.Incomplete = combined.Incomplete || completed != len(b.shards) || len(searchErrors) > 0
	if len(searchErrors) > 0 {
		return combined, errors.Join(searchErrors...)
	}
	if combined.Incomplete {
		return sampledProgressiveSearchResult(combined)
	}

	return combined, nil
}

func sampledProgressiveSearchResult(
	result SearchResultSet,
) (SearchResultSet, error) {
	if result.TotalShards > 0 && result.CompletedShards == result.TotalShards &&
		result.SampledShards > 0 {
		return result, nil
	}

	return result, fmt.Errorf("progressive search incomplete")
}

func (b *BleveDiskIndex) searchProgressiveShard(
	ctx context.Context,
	req SearchRequest,
	shardNumber int,
) (SearchResultSet, error) {
	shard := b.shards[shardNumber]
	shardIndex := &BleveDiskIndex{
		shards:              []bleve.Index{shard},
		alias:               newBleveDiskSearchAlias([]bleve.Index{shard}),
		documents:           b.documents,
		documentPresence:    b.documentPresence,
		multilingual:        b.multilingual,
		analyzerScope:       b.analyzerScope,
		storedCandidates:    b.storedCandidates,
		searchReadAdmission: b.searchReadAdmission,
	}
	shardRequest := req
	shardRequest.AllowCandidateSampling = true
	result, _, err := shardIndex.searchHits(ctx, shardRequest)

	return result, err
}

func mergeProgressiveShardResult(
	current SearchResultSet,
	shard SearchResultSet,
	maximumResults int,
	completedShards int,
) SearchResultSet {
	current.Total += shard.Total
	current.Incomplete = current.Incomplete || shard.Incomplete
	current.Results = mergeProgressiveShardResults(
		current.Results,
		shard.Results,
		maximumResults,
	)
	current.CompletedShards = completedShards

	return current
}

func randomShardOrder(
	shardCount int,
	randomBelow func(int) (int, error),
) ([]int, error) {
	if shardCount < 0 {
		return nil, fmt.Errorf("shard count cannot be negative")
	}
	order := make([]int, shardCount)
	for shard := range order {
		order[shard] = shard
	}
	if randomBelow == nil {
		randomBelow = cryptographicSearchRandomIndex
	}
	for last := len(order) - 1; last > 0; last-- {
		selected, err := randomBelow(last + 1)
		if err != nil {
			return nil, err
		}
		if selected < 0 || selected > last {
			return nil, fmt.Errorf("random shard index outside shuffle range")
		}
		order[last], order[selected] = order[selected], order[last]
	}

	return order, nil
}

func cryptographicSearchRandomIndex(maximum int) (int, error) {
	if maximum <= 0 {
		return 0, fmt.Errorf("random search shard range must be positive")
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maximum)))
	if err != nil {
		return 0, fmt.Errorf("read operating system random source: %w", err)
	}

	return int(value.Int64()), nil
}

func cryptographicSearchRandomDocumentID(maximum uint64) (uint64, error) {
	if maximum == 0 {
		return 0, fmt.Errorf("progressive document range must be positive")
	}
	value, err := cryptorand.Int(cryptorand.Reader, new(big.Int).SetUint64(maximum))
	if err != nil {
		return 0, fmt.Errorf("read operating system random source: %w", err)
	}

	return value.Uint64(), nil
}

func mergeProgressiveShardResults(
	current []SearchResult,
	shardResults []SearchResult,
	limit int,
) []SearchResult {
	if limit <= 0 {
		return nil
	}
	combined := append([]SearchResult(nil), current...)
	ordered := append([]SearchResult(nil), shardResults...)
	sort.SliceStable(ordered, func(first, second int) bool {
		if ordered[first].Score != ordered[second].Score {
			return ordered[first].Score > ordered[second].Score
		}

		return ordered[first].DocumentID < ordered[second].DocumentID
	})
	for rank := range ordered {
		ordered[rank].Score = 1 / float64(progressiveSearchReciprocalRankConstant+rank+1)
		combined = append(combined, ordered[rank])
	}
	sort.SliceStable(combined, func(first, second int) bool {
		if combined[first].Score != combined[second].Score {
			return combined[first].Score > combined[second].Score
		}

		return combined[first].DocumentID < combined[second].DocumentID
	})
	seen := make(map[string]struct{}, len(combined))
	deduplicated := make([]SearchResult, 0, min(limit, len(combined)))
	for _, result := range combined {
		if _, exists := seen[result.DocumentID]; exists {
			continue
		}
		seen[result.DocumentID] = struct{}{}
		deduplicated = append(deduplicated, result)
		if len(deduplicated) == limit {
			break
		}
	}

	return deduplicated
}
