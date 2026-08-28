package searchindex

import (
	"context"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

type bleveDiskSearchShardProbe struct {
	bleveIndexContract
	name   string
	result *bleve.SearchResult
	err    error
}

func (probe *bleveDiskSearchShardProbe) Name() string {
	return probe.name
}

func (probe *bleveDiskSearchShardProbe) SearchInContext(
	_ context.Context,
	_ *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	return probe.result, probe.err
}

func TestBleveDiskSearchAliasPreservesFailures(t *testing.T) {
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
