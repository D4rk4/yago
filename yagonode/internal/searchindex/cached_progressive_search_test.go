package searchindex

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type progressiveCountingIndex struct {
	*countingIndex
	request SearchRequest
	updates int
	err     error
}

func (index *progressiveCountingIndex) SearchProgressively(
	_ context.Context,
	request SearchRequest,
	notify func(SearchProgress),
) (SearchResultSet, error) {
	index.request = request
	progress := SearchProgress{CompletedShards: 1, TotalShards: 2}
	index.updates++
	if notify != nil {
		notify(progress)
	}

	return SearchResultSet{Incomplete: true, CompletedShards: 1, TotalShards: 2}, index.err
}

func TestCachedSearchIndexForwardsProgressiveSearchWithoutCaching(t *testing.T) {
	inner := &progressiveCountingIndex{countingIndex: &countingIndex{}}
	cache := NewCachedSearchIndex(inner, 4)
	request := SearchRequest{Query: "sample", MaxResults: 5, AllowCandidateSampling: true}
	var updates int
	result, err := cache.SearchProgressively(
		t.Context(), request, func(progress SearchProgress) {
			updates++
			if progress.CompletedShards != 1 || progress.TotalShards != 2 {
				t.Errorf("progress = %+v", progress)
			}
		},
	)
	if err != nil || result.CompletedShards != 1 || !result.Incomplete ||
		!reflect.DeepEqual(inner.request, request) || inner.updates != 1 || updates != 1 ||
		inner.searches != 0 || len(cache.entries) != 0 {
		t.Fatalf("result=%+v err=%v request=%+v updates=%d cached=%d",
			result, err, inner.request, updates, len(cache.entries))
	}
}

func TestCachedSearchIndexRejectsUnsupportedProgressiveSearch(t *testing.T) {
	cache := NewCachedSearchIndex(&countingIndex{}, 4)
	if _, err := cache.SearchProgressively(
		t.Context(), SearchRequest{Query: "sample"}, nil,
	); err == nil {
		t.Fatal("unsupported progressive search was accepted")
	}
}

func TestCachedSearchIndexPreservesProgressiveSearchFailure(t *testing.T) {
	sentinel := errors.New("progressive backend failed")
	inner := &progressiveCountingIndex{countingIndex: &countingIndex{}, err: sentinel}
	cache := NewCachedSearchIndex(inner, 4)
	result, err := cache.SearchProgressively(
		t.Context(), SearchRequest{Query: "sample"}, nil,
	)
	if !errors.Is(err, sentinel) || !result.Incomplete {
		t.Fatalf("progressive failure result=%+v error=%v", result, err)
	}
}
