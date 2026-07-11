package searchindex

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type countingIndex struct {
	searches  int
	indexes   int
	deletes   int
	closes    int
	results   SearchResultSet
	resultFor func(SearchRequest) SearchResultSet
	searchErr error
	writeErr  error
	closeErr  error
	statsErr  error
	onSearch  func()
}

func (c *countingIndex) Index(context.Context, documentstore.Document) error {
	c.indexes++

	return c.writeErr
}

func (c *countingIndex) Delete(context.Context, string) error {
	c.deletes++

	return c.writeErr
}

func (c *countingIndex) Search(_ context.Context, req SearchRequest) (SearchResultSet, error) {
	c.searches++
	if c.onSearch != nil {
		c.onSearch()
	}
	if c.resultFor != nil {
		return c.resultFor(req), c.searchErr
	}

	return c.results, c.searchErr
}

func (c *countingIndex) Stats(context.Context) (IndexStats, error) {
	return IndexStats{Documents: 7, Backend: "counting"}, c.statsErr
}

func (c *countingIndex) Close() error {
	c.closes++

	return c.closeErr
}

func cachedRequest() SearchRequest {
	return SearchRequest{Query: "golang", MaxResults: 5}
}

func TestCachedSearchIndexServesRepeatQueriesFromCache(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{
		Results: []SearchResult{{URL: "https://example.org/"}},
		Total:   1,
	}}
	cache := NewCachedSearchIndex(inner, 0)

	first, err := cache.Search(t.Context(), cachedRequest())
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	if len(first.Results) != 1 || first.Total != 1 {
		t.Fatalf("first = %#v", first)
	}

	first.Results[0].URL = "mutated"
	second, err := cache.Search(t.Context(), cachedRequest())
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if inner.searches != 1 {
		t.Fatalf("inner searches = %d, want 1 (cache hit)", inner.searches)
	}
	if second.Results[0].URL != "https://example.org/" {
		t.Fatalf("cache returned mutated entry: %#v", second.Results[0])
	}
}

func TestCachedSearchIndexInvalidatesOnWrites(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Total: 0}}
	cache := NewCachedSearchIndex(inner, 4)

	if _, err := cache.Search(t.Context(), cachedRequest()); err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := cache.Index(t.Context(), documentstore.Document{NormalizedURL: "u"}); err != nil {
		t.Fatalf("index: %v", err)
	}
	if _, err := cache.Search(t.Context(), cachedRequest()); err != nil {
		t.Fatalf("search after index: %v", err)
	}
	if err := cache.Delete(t.Context(), "u"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := cache.Search(t.Context(), cachedRequest()); err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if inner.searches != 3 {
		t.Fatalf("inner searches = %d, want 3 (each write invalidated)", inner.searches)
	}
}

func TestCachedSearchIndexDoesNotCacheAcrossConcurrentWrite(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Total: 0}}
	cache := NewCachedSearchIndex(inner, 4)
	inner.onSearch = func() {
		inner.onSearch = nil
		cache.invalidate()
	}

	if _, err := cache.Search(t.Context(), cachedRequest()); err != nil {
		t.Fatalf("first search: %v", err)
	}
	if _, err := cache.Search(t.Context(), cachedRequest()); err != nil {
		t.Fatalf("second search: %v", err)
	}
	if inner.searches != 2 {
		t.Fatalf("inner searches = %d, want 2 (first result not cached)", inner.searches)
	}
}

func TestCachedSearchIndexKeysOnWeightsAndExplain(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Total: 0}}
	cache := NewCachedSearchIndex(inner, 8)
	ctx := t.Context()
	base := SearchRequest{Query: "golang", MaxResults: 5}

	if _, err := cache.Search(ctx, base); err != nil {
		t.Fatalf("default search: %v", err)
	}
	weighted := base
	weighted.Weights = RankingWeights{Title: 9, Headings: 1, Anchors: 1, Body: 1, URL: 1}
	if _, err := cache.Search(ctx, weighted); err != nil {
		t.Fatalf("weighted search: %v", err)
	}
	explained := base
	explained.Explain = true
	if _, err := cache.Search(ctx, explained); err != nil {
		t.Fatalf("explained search: %v", err)
	}
	if inner.searches != 3 {
		t.Fatalf(
			"inner searches = %d, want 3 (weights and explain are distinct keys)",
			inner.searches,
		)
	}
}

func TestCachedSearchIndexNormalisesDefaultWeights(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Total: 0}}
	cache := NewCachedSearchIndex(inner, 8)
	ctx := t.Context()

	if _, err := cache.Search(ctx, SearchRequest{Query: "go", MaxResults: 5}); err != nil {
		t.Fatalf("zero-weight search: %v", err)
	}
	explicit := SearchRequest{Query: "go", MaxResults: 5, Weights: DefaultRankingWeights()}
	if _, err := cache.Search(ctx, explicit); err != nil {
		t.Fatalf("explicit-default search: %v", err)
	}
	if inner.searches != 1 {
		t.Fatalf(
			"inner searches = %d, want 1 (zero and explicit default share a key)",
			inner.searches,
		)
	}
}

func TestCachedSearchIndexEvictsOldestBeyondCapacity(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Total: 0}}
	cache := NewCachedSearchIndex(inner, 1)

	if _, err := cache.Search(t.Context(), SearchRequest{Query: "a", MaxResults: 1}); err != nil {
		t.Fatalf("search a: %v", err)
	}
	if _, err := cache.Search(t.Context(), SearchRequest{Query: "b", MaxResults: 1}); err != nil {
		t.Fatalf("search b: %v", err)
	}
	if _, err := cache.Search(t.Context(), SearchRequest{Query: "a", MaxResults: 1}); err != nil {
		t.Fatalf("search a again: %v", err)
	}
	if inner.searches != 3 {
		t.Fatalf("inner searches = %d, want 3 (a evicted by b)", inner.searches)
	}
}

func TestCachedSearchIndexPropagatesErrorsAndSkipsInvalidation(t *testing.T) {
	sentinel := errors.New("boom")
	inner := &countingIndex{searchErr: sentinel, writeErr: sentinel}
	cache := NewCachedSearchIndex(inner, 4)

	if _, err := cache.Search(t.Context(), cachedRequest()); !errors.Is(err, sentinel) {
		t.Fatalf("search error = %v, want sentinel", err)
	}
	if err := cache.Index(t.Context(), documentstore.Document{}); !errors.Is(err, sentinel) {
		t.Fatalf("index error = %v, want sentinel", err)
	}
	if err := cache.Delete(t.Context(), "u"); !errors.Is(err, sentinel) {
		t.Fatalf("delete error = %v, want sentinel", err)
	}
	if cache.generation != 0 {
		t.Fatalf("generation = %d, want 0 (failed writes do not invalidate)", cache.generation)
	}
}

func TestCachedSearchIndexForwardsStats(t *testing.T) {
	cache := NewCachedSearchIndex(&countingIndex{}, 4)
	stats, err := cache.Stats(t.Context())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Documents != 7 || stats.Backend != "counting" {
		t.Fatalf("stats = %#v", stats)
	}

	sentinel := errors.New("stats boom")
	if _, err := NewCachedSearchIndex(
		&countingIndex{statsErr: sentinel},
		4,
	).Stats(t.Context()); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("stats error = %v, want sentinel", err)
	}
}

func TestCachedSearchIndexForwardsClose(t *testing.T) {
	sentinel := errors.New("close boom")
	closer := &countingIndex{closeErr: sentinel}
	cache := NewCachedSearchIndex(closer, 4)
	if err := cache.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("close error = %v, want sentinel", err)
	}
	if closer.closes != 1 {
		t.Fatalf("inner closes = %d, want 1", closer.closes)
	}

	okCloser := &countingIndex{}
	if err := NewCachedSearchIndex(okCloser, 4).Close(); err != nil {
		t.Fatalf("close = %v, want nil", err)
	}

	if err := NewCachedSearchIndex(fakeIndex{}, 4).Close(); err != nil {
		t.Fatalf("non-closer close = %v, want nil", err)
	}
}

func TestCachedSearchIndexKeysOnPhrases(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Total: 0}}
	cache := NewCachedSearchIndex(inner, 8)
	ctx := t.Context()
	base := SearchRequest{Query: "quick brown", MaxResults: 5}

	if _, err := cache.Search(ctx, base); err != nil {
		t.Fatalf("base search: %v", err)
	}
	phrased := base
	phrased.Phrases = []string{"quick brown"}
	if _, err := cache.Search(ctx, phrased); err != nil {
		t.Fatalf("phrased search: %v", err)
	}
	if _, err := cache.Search(ctx, phrased); err != nil {
		t.Fatalf("phrased repeat: %v", err)
	}
	if inner.searches != 2 {
		t.Fatalf(
			"inner searches = %d, want 2 (phrases are a distinct key; repeat is cached)",
			inner.searches,
		)
	}
}
