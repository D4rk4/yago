package searchindex

import (
	"reflect"
	"testing"
	"time"
)

func TestCacheKeyCoversEverySearchRequestField(t *testing.T) {
	base := completeCachedSearchRequest()
	variants := cacheRequestFieldVariants(base)
	requestType := reflect.TypeFor[SearchRequest]()
	if len(variants) != requestType.NumField() {
		t.Fatalf("variants = %d, SearchRequest fields = %d", len(variants), requestType.NumField())
	}
	for index := range requestType.NumField() {
		field := requestType.Field(index).Name
		if _, found := variants[field]; !found {
			t.Fatalf("SearchRequest field %s has no cache-key regression variant", field)
		}
	}

	baseKey := cacheKey(base)
	for field, variant := range variants {
		if cacheKey(variant) == baseKey {
			t.Errorf("SearchRequest field %s did not change the cache key", field)
		}
	}
}

func TestCacheKeyLengthPrefixesStringSlices(t *testing.T) {
	left := cachedRequest()
	left.Terms = []string{"alpha\x1fbeta"}
	right := cachedRequest()
	right.Terms = []string{"alpha", "beta"}
	if cacheKey(left) == cacheKey(right) {
		t.Fatal("distinct term slices produced the same cache key")
	}
}

func TestCacheKeyCoversEveryRankingWeight(t *testing.T) {
	base := completeCachedSearchRequest()
	weightType := reflect.TypeFor[RankingWeights]()
	variants := make(map[string]RankingWeights, weightType.NumField())
	for index := range weightType.NumField() {
		weights := base.Weights
		value := reflect.ValueOf(&weights).Elem().Field(index)
		value.SetFloat(value.Float() + 1)
		variants[weightType.Field(index).Name] = weights
	}
	if len(variants) != weightType.NumField() {
		t.Fatalf("variants = %d, RankingWeights fields = %d", len(variants), weightType.NumField())
	}

	baseKey := cacheKey(base)
	for name, weights := range variants {
		variant := base
		variant.Weights = weights
		if cacheKey(variant) == baseKey {
			t.Errorf("RankingWeights field %s did not change the cache key", name)
		}
	}
}

func TestCacheKeyCanonicalizesTimeZones(t *testing.T) {
	left := completeCachedSearchRequest()
	right := left
	right.Since = left.Since.UTC()
	if cacheKey(left) != cacheKey(right) {
		t.Fatal("equal instants in different time zones produced different cache keys")
	}
}

func TestCachedSearchIndexSeparatesFuzzyRetry(t *testing.T) {
	inner := &countingIndex{resultFor: func(req SearchRequest) SearchResultSet {
		if req.Fuzzy {
			return SearchResultSet{
				Results: []SearchResult{{URL: "https://fuzzy.example/"}},
				Total:   1,
			}
		}

		return SearchResultSet{}
	}}
	cache := NewCachedSearchIndex(inner, 4)
	req := cachedRequest()

	exact := cachedSearch(t, cache, req)
	if len(exact.Results) != 0 {
		t.Fatalf("exact results = %d, want 0", len(exact.Results))
	}
	req.Fuzzy = true
	fuzzy := cachedSearch(t, cache, req)
	if len(fuzzy.Results) != 1 || fuzzy.Results[0].URL != "https://fuzzy.example/" {
		t.Fatalf("fuzzy results = %#v", fuzzy.Results)
	}
	cachedSearch(t, cache, req)
	if inner.searches != 2 {
		t.Fatalf("inner searches = %d, want separate exact and fuzzy searches", inner.searches)
	}
}

func TestCachedSearchIndexSeparatesExpansionPass(t *testing.T) {
	inner := &countingIndex{resultFor: func(req SearchRequest) SearchResultSet {
		url := "https://first.example/"
		if len(req.ExpansionTerms) > 0 {
			url = "https://expanded.example/"
		}

		return SearchResultSet{Results: []SearchResult{{URL: url}}, Total: 1}
	}}
	cache := NewCachedSearchIndex(inner, 4)
	req := cachedRequest()
	first := cachedSearch(t, cache, req)
	req.ExpansionTerms = []string{"compiler"}
	expanded := cachedSearch(t, cache, req)
	if first.Results[0].URL == expanded.Results[0].URL {
		t.Fatalf("expansion pass reused first-pass result: %#v", expanded.Results)
	}
	if inner.searches != 2 {
		t.Fatalf("inner searches = %d, want separate PRF passes", inner.searches)
	}
}

func TestCachedSearchIndexSeparatesFilters(t *testing.T) {
	base := cachedRequest()
	filterVariants := map[string]SearchRequest{}
	addCacheVariant(filterVariants, "IncludeDomain", base, func(req *SearchRequest) {
		req.IncludeDomain = []string{"example.org"}
	})
	addCacheVariant(filterVariants, "ExcludeDomain", base, func(req *SearchRequest) {
		req.ExcludeDomain = []string{"blocked.example"}
	})
	addCacheVariant(filterVariants, "Language", base, func(req *SearchRequest) {
		req.Language = "de"
	})
	addCacheVariant(filterVariants, "Since", base, func(req *SearchRequest) {
		req.Since = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	})
	addCacheVariant(filterVariants, "Until", base, func(req *SearchRequest) {
		req.Until = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	})
	addCacheVariant(filterVariants, "Author", base, func(req *SearchRequest) {
		req.Author = "Ada"
	})
	addCacheVariant(filterVariants, "ContentDomain", base, func(req *SearchRequest) {
		req.ContentDomain = "image"
	})
	addCacheVariant(filterVariants, "MinDate", base, func(req *SearchRequest) {
		req.MinDate = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	})
	addCacheVariant(filterVariants, "MaxDate", base, func(req *SearchRequest) {
		req.MaxDate = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	})
	addCacheVariant(filterVariants, "FileType", base, func(req *SearchRequest) {
		req.FileType = "pdf"
	})
	addCacheVariant(filterVariants, "InURL", base, func(req *SearchRequest) {
		req.InURL = "guide"
	})
	addCacheVariant(filterVariants, "TLD", base, func(req *SearchRequest) {
		req.TLD = "org"
	})

	for name, variant := range filterVariants {
		t.Run(name, func(t *testing.T) {
			inner := &countingIndex{}
			cache := NewCachedSearchIndex(inner, 4)
			cachedSearch(t, cache, base)
			cachedSearch(t, cache, variant)
			if inner.searches != 2 {
				t.Fatalf("inner searches = %d, want distinct filter entries", inner.searches)
			}
		})
	}
}

func TestCachedSearchIndexSeparatesTermsNearAndFacets(t *testing.T) {
	base := cachedRequest()
	variants := map[string]SearchRequest{}
	addCacheVariant(variants, "Terms", base, func(req *SearchRequest) {
		req.Terms = []string{"golang", "cache"}
	})
	addCacheVariant(variants, "Near", base, func(req *SearchRequest) { req.Near = true })
	addCacheVariant(variants, "WithFacets", base, func(req *SearchRequest) {
		req.WithFacets = true
	})

	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			inner := &countingIndex{}
			cache := NewCachedSearchIndex(inner, 4)
			cachedSearch(t, cache, base)
			cachedSearch(t, cache, variant)
			if inner.searches != 2 {
				t.Fatalf("inner searches = %d, want distinct request entries", inner.searches)
			}
		})
	}
}

func TestCachedSearchIndexPreservesFacetsAndNestedMutationIsolation(t *testing.T) {
	inner := &countingIndex{results: SearchResultSet{
		Facets: []FacetGroup{{
			Name:  "host",
			Terms: []FacetTerm{{Term: "example.org", Count: 2}},
		}},
		Results: []SearchResult{{
			URL:         "https://example.org/",
			FieldScores: map[string]float64{"body": 1.5},
			FieldTermPositions: map[string]map[string][]int{
				"body":  {"golang": {2, 4}},
				"title": nil,
			},
			Images: []ResultImage{{URL: "https://example.org/image.png", Alt: "image"}},
		}},
		Total: 1,
	}}
	cache := NewCachedSearchIndex(inner, 4)
	req := cachedRequest()
	req.WithFacets = true

	first := cachedSearch(t, cache, req)
	mutateCachedResult(first)
	second := cachedSearch(t, cache, req)
	assertOriginalCachedResult(t, second)
	mutateCachedResult(second)
	third := cachedSearch(t, cache, req)
	assertOriginalCachedResult(t, third)
	if inner.searches != 1 {
		t.Fatalf("inner searches = %d, want repeated cache hits", inner.searches)
	}
}

func completeCachedSearchRequest() SearchRequest {
	return SearchRequest{
		Query:              "alpha beta",
		ExcludeTerms:       []string{"gamma"},
		Phrases:            []string{"alpha beta"},
		MaxResults:         20,
		IncludeRaw:         true,
		SafeSearch:         true,
		IncludeDomain:      []string{"example.org"},
		ExcludeDomain:      []string{"blocked.example"},
		Language:           "en",
		Since:              time.Date(2025, 1, 1, 0, 0, 0, 0, time.FixedZone("west", -3600)),
		Until:              time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		Weights:            DefaultRankingWeights(),
		Explain:            true,
		IncludeFieldScores: true,
		IncludePositions:   true,
		Fuzzy:              true,
		Author:             "Ada",
		Terms:              []string{"alpha", "beta"},
		Near:               true,
		ExpansionTerms:     []string{"compiler"},
		MinimumTermMatches: 1,
		WithFacets:         true,
		ContentDomain:      "text",
		MinDate:            time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		MaxDate:            time.Date(2025, 11, 30, 0, 0, 0, 0, time.UTC),
		FileType:           "html",
		InURL:              "guide",
		TLD:                "org",
	}
}

func cacheRequestFieldVariants(base SearchRequest) map[string]SearchRequest {
	variants := cacheRequestRetrievalFieldVariants(base)
	for name, variant := range cacheRequestFilterFieldVariants(base) {
		variants[name] = variant
	}

	return variants
}

func cacheRequestRetrievalFieldVariants(base SearchRequest) map[string]SearchRequest {
	variants := map[string]SearchRequest{}
	addCacheVariant(variants, "Query", base, func(req *SearchRequest) {
		req.Query = "delta"
	})
	addCacheVariant(variants, "ExcludeTerms", base, func(req *SearchRequest) {
		req.ExcludeTerms = []string{"delta"}
	})
	addCacheVariant(variants, "Phrases", base, func(req *SearchRequest) {
		req.Phrases = []string{"beta alpha"}
	})
	addCacheVariant(variants, "MaxResults", base, func(req *SearchRequest) {
		req.MaxResults++
	})
	addCacheVariant(variants, "IncludeRaw", base, func(req *SearchRequest) {
		req.IncludeRaw = false
	})
	addCacheVariant(variants, "SafeSearch", base, func(req *SearchRequest) {
		req.SafeSearch = false
	})
	addCacheVariant(variants, "IncludeDomain", base, func(req *SearchRequest) {
		req.IncludeDomain = []string{"other.example"}
	})
	addCacheVariant(variants, "ExcludeDomain", base, func(req *SearchRequest) {
		req.ExcludeDomain = []string{"other.example"}
	})
	addCacheVariant(variants, "Language", base, func(req *SearchRequest) {
		req.Language = "de"
	})
	addCacheVariant(variants, "Since", base, func(req *SearchRequest) {
		req.Since = req.Since.Add(time.Hour)
	})
	addCacheVariant(variants, "Until", base, func(req *SearchRequest) {
		req.Until = req.Until.Add(time.Hour)
	})
	addCacheVariant(variants, "Weights", base, func(req *SearchRequest) {
		req.Weights.Title++
	})

	return variants
}

func cacheRequestFilterFieldVariants(base SearchRequest) map[string]SearchRequest {
	variants := map[string]SearchRequest{}
	addCacheVariant(variants, "Explain", base, func(req *SearchRequest) {
		req.Explain = false
	})
	addCacheVariant(variants, "IncludeFieldScores", base, func(req *SearchRequest) {
		req.IncludeFieldScores = false
	})
	addCacheVariant(variants, "IncludePositions", base, func(req *SearchRequest) {
		req.IncludePositions = false
	})
	addCacheVariant(variants, "CandidateOnly", base, func(req *SearchRequest) {
		req.CandidateOnly = true
	})
	addCacheVariant(variants, "Fuzzy", base, func(req *SearchRequest) {
		req.Fuzzy = false
	})
	addCacheVariant(variants, "Relaxed", base, func(req *SearchRequest) {
		req.Relaxed = true
	})
	addCacheVariant(variants, "Author", base, func(req *SearchRequest) {
		req.Author = "Grace"
	})
	addCacheVariant(variants, "Terms", base, func(req *SearchRequest) {
		req.Terms = []string{"delta"}
	})
	addCacheVariant(variants, "Near", base, func(req *SearchRequest) {
		req.Near = false
	})
	addCacheVariant(variants, "ExpansionTerms", base, func(req *SearchRequest) {
		req.ExpansionTerms = []string{"runtime"}
	})
	addCacheVariant(variants, "MinimumTermMatches", base, func(req *SearchRequest) {
		req.MinimumTermMatches++
	})
	addCacheVariant(variants, "WithFacets", base, func(req *SearchRequest) {
		req.WithFacets = false
	})
	addCacheVariant(variants, "ContentDomain", base, func(req *SearchRequest) {
		req.ContentDomain = "image"
	})
	addCacheVariant(variants, "MinDate", base, func(req *SearchRequest) {
		req.MinDate = req.MinDate.Add(time.Hour)
	})
	addCacheVariant(variants, "MaxDate", base, func(req *SearchRequest) {
		req.MaxDate = req.MaxDate.Add(time.Hour)
	})
	addCacheVariant(variants, "FileType", base, func(req *SearchRequest) {
		req.FileType = "pdf"
	})
	addCacheVariant(variants, "InURL", base, func(req *SearchRequest) {
		req.InURL = "reference"
	})
	addCacheVariant(variants, "TLD", base, func(req *SearchRequest) {
		req.TLD = "net"
	})

	return variants
}

func addCacheVariant(
	variants map[string]SearchRequest,
	name string,
	base SearchRequest,
	change func(*SearchRequest),
) {
	change(&base)
	variants[name] = base
}

func cachedSearch(t *testing.T, cache *CachedSearchIndex, req SearchRequest) SearchResultSet {
	t.Helper()
	result, err := cache.Search(t.Context(), req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	return result
}

func mutateCachedResult(set SearchResultSet) {
	set.Facets[0].Name = "changed"
	set.Facets[0].Terms[0] = FacetTerm{Term: "changed", Count: 99}
	set.Results[0].URL = "https://changed.example/"
	set.Results[0].FieldScores["body"] = 99
	set.Results[0].FieldTermPositions["body"]["golang"][0] = 99
	set.Results[0].FieldTermPositions["body"]["new"] = []int{99}
	set.Results[0].FieldTermPositions["title"] = map[string][]int{"changed": {99}}
	set.Results[0].Images[0] = ResultImage{URL: "https://changed.example/image.png"}
}

func assertOriginalCachedResult(t *testing.T, set SearchResultSet) {
	t.Helper()
	if set.Total != 1 || len(set.Facets) != 1 || set.Facets[0].Name != "host" ||
		len(set.Facets[0].Terms) != 1 || set.Facets[0].Terms[0] != (FacetTerm{
		Term: "example.org", Count: 2,
	}) {
		t.Fatalf("facets = %#v", set.Facets)
	}
	result := set.Results[0]
	if result.URL != "https://example.org/" || result.FieldScores["body"] != 1.5 ||
		!reflect.DeepEqual(result.FieldTermPositions["body"]["golang"], []int{2, 4}) ||
		len(result.FieldTermPositions["body"]) != 1 || result.FieldTermPositions["title"] != nil ||
		!reflect.DeepEqual(result.Images, []ResultImage{{
			URL: "https://example.org/image.png", Alt: "image",
		}}) {
		t.Fatalf("result = %#v", result)
	}
}
