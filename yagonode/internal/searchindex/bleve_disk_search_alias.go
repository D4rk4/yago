package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
)

type bleveDiskSearchShardPosition struct{}

type bleveDiskSearchIndex = bleve.Index

type bleveDiskSearchShard struct {
	bleveDiskSearchIndex
	position int
}

func newBleveDiskSearchAlias(shards []bleve.Index) bleve.Index {
	searchShards := make([]bleve.Index, len(shards))
	for position, shard := range shards {
		searchShards[position] = bleveDiskSearchShard{
			bleveDiskSearchIndex: shard,
			position:             position,
		}
	}

	return bleve.NewIndexAlias(searchShards...)
}

func (shard bleveDiskSearchShard) SearchInContext(
	ctx context.Context,
	request *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	result, err := shard.bleveDiskSearchIndex.SearchInContext(
		context.WithValue(ctx, bleveDiskSearchShardPosition{}, shard.position),
		request,
	)
	if err != nil {
		return nil, fmt.Errorf("search disk shard: %w", err)
	}

	return result, nil
}
