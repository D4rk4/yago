package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/searcher"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveLexicalCandidateShardQuery struct {
	shardIdentities [][]string
	allIdentities   []string
}

func bleveLexicalCandidateIdentityQuery(
	shards []bleve.Index,
	candidates search.DocumentMatchCollection,
) blevequery.Query {
	allIdentities := lexicalCandidateIdentities(candidates)
	shardIdentities, recognized := lexicalCandidateShardIdentities(shards, candidates)
	if recognized {
		return &bleveLexicalCandidateShardQuery{
			shardIdentities: shardIdentities,
			allIdentities:   allIdentities,
		}
	}

	identityQuery := bleve.NewDocIDQuery(allIdentities)
	identityQuery.SetBoost(0)

	return identityQuery
}

func lexicalCandidateIdentities(
	candidates search.DocumentMatchCollection,
) []string {
	identities := make([]string, len(candidates))
	for position, candidate := range candidates {
		identities[position] = candidate.ID
	}

	return identities
}

func lexicalCandidateShardIdentities(
	shards []bleve.Index,
	candidates search.DocumentMatchCollection,
) ([][]string, bool) {
	if len(shards) == 0 {
		return nil, false
	}
	positions := make(map[string]int, len(shards))
	for position, shard := range shards {
		if shard == nil {
			return nil, false
		}
		name := shard.Name()
		if name == "" {
			return nil, false
		}
		if _, found := positions[name]; found {
			return nil, false
		}
		positions[name] = position
	}

	identities := make([][]string, len(shards))
	for _, candidate := range candidates {
		if candidate == nil {
			return nil, false
		}
		position, found := positions[candidate.Index]
		if !found {
			return nil, false
		}
		identities[position] = append(identities[position], candidate.ID)
	}

	return identities, true
}

func (query *bleveLexicalCandidateShardQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	_ mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	identities := query.allIdentities
	position, recognized := ctx.Value(bleveDiskSearchShardPosition{}).(int)
	if recognized && position >= 0 && position < len(query.shardIdentities) {
		identities = query.shardIdentities[position]
	}

	candidateSearcher, err := searcher.NewDocIDSearcher(
		ctx,
		reader,
		identities,
		0,
		options,
	)
	if err != nil {
		return nil, fmt.Errorf("select lexical candidate identities: %w", err)
	}

	return candidateSearcher, nil
}

func (query *bleveLexicalCandidateShardQuery) Field() string {
	return "_id"
}

func (query *bleveLexicalCandidateShardQuery) SetField(string) {}
