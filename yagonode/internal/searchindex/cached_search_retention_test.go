package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestRetainedSearchEntryBytesAccountsCompletePayload(t *testing.T) {
	positions := make([]int, 2, 3)
	matches := make([]TextQueryMatch, 1, 2)
	matches[0] = TextQueryMatch{Start: 1, End: 2}
	images := make([]ResultImage, 1, 2)
	images[0] = ResultImage{URL: "x", Alt: "x"}
	results := make([]SearchResult, 1, 2)
	results[0] = SearchResult{
		DocumentID:         "x",
		ClusterID:          "x",
		RepresentativeURL:  "x",
		Title:              "x",
		URL:                "x",
		Snippet:            "x",
		RawContent:         "x",
		Explanation:        "x",
		Author:             "x",
		Keywords:           "x",
		Publisher:          "x",
		Language:           "x",
		Analyzer:           "x",
		ContentType:        "x",
		Images:             images,
		BodyQueryMatches:   matches,
		FieldScores:        map[string]float64{"x": 1},
		FieldTermPositions: map[string]map[string][]int{"x": {"x": positions}},
	}
	terms := make([]FacetTerm, 1, 2)
	terms[0] = FacetTerm{Term: "x", Count: 1}
	facets := make([]FacetGroup, 1, 2)
	facets[0] = FacetGroup{Name: "x", Terms: terms}
	entry := &cachedSearchEntry{
		key: "x", results: SearchResultSet{Results: results, Facets: facets},
	}

	want := int(retainedSearchEntryWidth + retainedSearchListElementWidth)
	want++
	want += cap(results) * int(retainedSearchResultWidth)
	want += 14
	want += cap(images)*int(retainedSearchImageWidth) + 2
	want += cap(matches) * int(retainedSearchQueryMatchWidth)
	want += retainedSearchMapBytes + retainedSearchMapEntryBytes + 1
	want += retainedSearchMapBytes + retainedSearchMapEntryBytes + 1
	want += retainedSearchMapBytes + retainedSearchMapEntryBytes + 1
	want += cap(positions) * int(retainedSearchPositionWidth)
	want += cap(facets)*int(retainedSearchFacetGroupWidth) + 1
	want += cap(terms)*int(retainedSearchFacetTermWidth) + 1
	if got := retainedSearchEntryBytes(entry); got != want {
		t.Fatalf("retained bytes = %d, want %d", got, want)
	}
}

func TestCachedSearchIndexByteLimitUsesTrueLRU(t *testing.T) {
	inner := &countingIndex{resultFor: func(req SearchRequest) SearchResultSet {
		return SearchResultSet{Results: []SearchResult{{
			Title: req.Query, RawContent: strings.Repeat("x", 1024),
		}}}
	}}
	cache := NewCachedSearchIndex(inner, 8)
	a := SearchRequest{Query: "a", MaxResults: 1}
	b := SearchRequest{Query: "b", MaxResults: 1}
	c := SearchRequest{Query: "c", MaxResults: 1}
	cachedSearch(t, cache, a)
	cachedSearch(t, cache, b)
	cache.limit = cache.retained
	cachedSearch(t, cache, a)
	cachedSearch(t, cache, c)
	if cache.entries[cacheKey(a)] == nil || cache.entries[cacheKey(c)] == nil ||
		cache.entries[cacheKey(b)] != nil || cache.retained > cache.limit {
		t.Fatalf("LRU entries/bytes = %#v/%d", cache.entries, cache.retained)
	}
	if inner.searches != 3 {
		t.Fatalf("inner searches = %d, want 3", inner.searches)
	}
	cachedSearch(t, cache, b)
	if inner.searches != 4 {
		t.Fatalf("evicted entry searches = %d, want 4", inner.searches)
	}
}

func TestCachedSearchIndexDoesNotRetainOversizedEntry(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Results: []SearchResult{{
		RawContent: strings.Repeat("x", 1024),
	}}}}
	cache := NewCachedSearchIndex(inner, 8)
	cache.limit = 1
	first := cachedSearch(t, cache, cachedRequest())
	second := cachedSearch(t, cache, cachedRequest())
	if len(first.Results) != 1 || len(second.Results) != 1 || inner.searches != 2 ||
		len(cache.entries) != 0 || cache.order.Len() != 0 || cache.retained != 0 {
		t.Fatalf(
			"oversized results/searches/entries/order/bytes = %d/%d/%d/%d/%d/%d",
			len(first.Results),
			len(second.Results),
			inner.searches,
			len(cache.entries),
			cache.order.Len(),
			cache.retained,
		)
	}
}

func TestCachedSearchIndexInvalidationReleasesRetainedEntries(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{Results: []SearchResult{{URL: "x"}}}}
	cache := NewCachedSearchIndex(inner, 8)
	cachedSearch(t, cache, cachedRequest())
	if cache.retained == 0 {
		t.Fatal("search entry was not retained")
	}
	if err := cache.Index(t.Context(), documentstore.Document{NormalizedURL: "x"}); err != nil {
		t.Fatal(err)
	}
	if cache.generation != 1 || len(cache.entries) != 0 || cache.order.Len() != 0 ||
		cache.retained != 0 {
		t.Fatalf(
			"index invalidation = %d/%d/%d/%d",
			cache.generation,
			len(cache.entries),
			cache.order.Len(),
			cache.retained,
		)
	}
	cachedSearch(t, cache, cachedRequest())
	if err := cache.Delete(t.Context(), "x"); err != nil {
		t.Fatal(err)
	}
	if cache.generation != 2 || len(cache.entries) != 0 || cache.order.Len() != 0 ||
		cache.retained != 0 {
		t.Fatalf(
			"delete invalidation = %d/%d/%d/%d",
			cache.generation,
			len(cache.entries),
			cache.order.Len(),
			cache.retained,
		)
	}
}

func TestCachedSearchIndexReplacementReconcilesRetainedBytes(t *testing.T) {
	cache := NewCachedSearchIndex(&countingIndex{}, 8)
	cache.store("key", 0, SearchResultSet{Results: []SearchResult{{Title: "x"}}})
	first := cache.retained
	cache.store("key", 0, SearchResultSet{Results: []SearchResult{{
		Title: "x", RawContent: strings.Repeat("x", 1024),
	}}})
	entry := cache.entries["key"]
	if entry == nil || cache.order.Len() != 1 || len(cache.entries) != 1 ||
		cache.retained != entry.retained || cache.retained <= first {
		t.Fatalf(
			"replacement entry/order/bytes = %#v/%d/%d",
			entry,
			cache.order.Len(),
			cache.retained,
		)
	}
	cache.limit = first
	cache.store("key", 0, SearchResultSet{Results: []SearchResult{{
		RawContent: strings.Repeat("x", 2048),
	}}})
	if len(cache.entries) != 0 || cache.order.Len() != 0 || cache.retained != 0 {
		t.Fatalf(
			"oversized replacement retained = %d/%d/%d",
			len(cache.entries),
			cache.order.Len(),
			cache.retained,
		)
	}
}

func TestRetainedSearchByteArithmeticSaturates(t *testing.T) {
	if got := retainedSearchProduct(retainedSearchMaximumInt, 2); got != retainedSearchMaximumInt {
		t.Fatalf("product = %d", got)
	}
	if got := retainedSearchAdd(retainedSearchMaximumInt, 1); got != retainedSearchMaximumInt {
		t.Fatalf("sum = %d", got)
	}
}

func TestCachedSearchIndexDropsEntryInvalidatedDuringDetachment(t *testing.T) {
	cache := NewCachedSearchIndex(&countingIndex{}, 8)
	previous := detachCachedSearchResultSet
	t.Cleanup(func() { detachCachedSearchResultSet = previous })
	detachCachedSearchResultSet = func(results SearchResultSet) SearchResultSet {
		cache.invalidate()

		return cloneResultSet(results)
	}
	cache.store("key", 0, SearchResultSet{Results: []SearchResult{{URL: "x"}}})
	if cache.generation != 1 || len(cache.entries) != 0 || cache.retained != 0 {
		t.Fatalf(
			"stale detached entry retained: %d/%d/%d",
			cache.generation,
			len(cache.entries),
			cache.retained,
		)
	}
}

func TestCloneResultSetPreservesNilNestedCollections(t *testing.T) {
	cloned := cloneResultSet(SearchResultSet{
		Facets: []FacetGroup{{Name: "host"}},
		Results: []SearchResult{{
			FieldTermPositions: map[string]map[string][]int{
				"body": {"term": nil},
			},
		}},
	})
	if cloned.Facets[0].Terms != nil || cloned.Results[0].Images != nil ||
		cloned.Results[0].BodyQueryMatches != nil ||
		cloned.Results[0].FieldTermPositions["body"]["term"] != nil {
		t.Fatalf("nil collections changed: %#v", cloned)
	}
}
