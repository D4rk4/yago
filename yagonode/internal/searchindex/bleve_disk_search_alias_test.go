package searchindex

import (
	"context"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

type bleveDiskSearchShardProbe struct {
	bleveIndexContract
	name       string
	result     *bleve.SearchResult
	err        error
	position   int
	recognized bool
}

func (probe *bleveDiskSearchShardProbe) Name() string {
	return probe.name
}

func (probe *bleveDiskSearchShardProbe) SearchInContext(
	ctx context.Context,
	_ *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	probe.position, probe.recognized = ctx.Value(bleveDiskSearchShardPosition{}).(int)

	return probe.result, probe.err
}

func TestBleveDiskSearchAliasAssignsShardPositionsAndPreservesFailures(t *testing.T) {
	sentinel := errors.New("shard search failed")
	first := &bleveDiskSearchShardProbe{
		name: "first",
		result: &bleve.SearchResult{
			Status: &bleve.SearchStatus{Total: 1, Successful: 1},
		},
	}
	second := &bleveDiskSearchShardProbe{name: "second", err: sentinel}
	alias := newBleveDiskSearchAlias([]bleve.Index{first, second})
	result, err := alias.SearchInContext(
		t.Context(),
		bleve.NewSearchRequest(bleve.NewMatchNoneQuery()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !first.recognized || first.position != 0 ||
		!second.recognized || second.position != 1 {
		t.Fatalf(
			"shard positions = %d/%t %d/%t",
			first.position,
			first.recognized,
			second.position,
			second.recognized,
		)
	}
	if result.Status.Total != 2 || result.Status.Successful != 1 ||
		result.Status.Failed != 1 || !errors.Is(result.Status.Errors["second"], sentinel) {
		t.Fatalf("search status = %#v", result.Status)
	}

	empty := newBleveDiskSearchAlias(nil)
	if _, err := empty.SearchInContext(
		t.Context(),
		bleve.NewSearchRequest(bleve.NewMatchNoneQuery()),
	); !errors.Is(err, bleve.ErrorAliasEmpty) {
		t.Fatalf("empty alias error = %v", err)
	}
}
