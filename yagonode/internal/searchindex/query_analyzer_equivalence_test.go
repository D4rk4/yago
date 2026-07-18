package searchindex

import (
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	blevequery "github.com/blevesearch/bleve/v2/search/query"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestRequirementAnalyzerBranchesCollapseExactLatinForms(t *testing.T) {
	req := SearchRequest{
		Query: "check point api",
		Terms: []string{"check", "point", "api"},
	}
	analyzers := queryAnalyzers(req.Query)
	branches, equivalent := requirementAnalyzerBranches(req, analyzers)
	if len(equivalent) == 0 || !slices.Contains(equivalent, standardTextAnalyzer) ||
		slices.Contains(branches, standardTextAnalyzer) ||
		len(branches) >= len(analyzers) {
		t.Fatalf(
			"branches = %v, equivalent = %v, analyzers = %v",
			branches,
			equivalent,
			analyzers,
		)
	}
}

func TestRequirementAnalyzerBranchesRetainChangedAndDroppedTerms(t *testing.T) {
	for _, test := range []struct {
		terms    []string
		analyzer string
	}{
		{terms: []string{"houses"}, analyzer: searchTextAnalyzer},
		{terms: []string{"talot"}, analyzer: "fi"},
		{terms: []string{"the", "cat"}, analyzer: searchTextAnalyzer},
	} {
		req := SearchRequest{Query: test.terms[0], Terms: test.terms}
		branches, _ := requirementAnalyzerBranches(req, queryAnalyzers(req.Query))
		if !slices.Contains(branches, test.analyzer) {
			t.Fatalf("terms %v branches = %v; missing %q", test.terms, branches, test.analyzer)
		}
	}
}

func TestAnalyzerEquivalenceFallsBackWhenMappingIsUnavailable(t *testing.T) {
	previous := loadStemmingMapping
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	t.Cleanup(func() { loadStemmingMapping = previous })

	analyzers := []string{"en", standardTextAnalyzer}
	branches, equivalent := requirementAnalyzerBranches(
		SearchRequest{Query: "query"},
		analyzers,
	)
	if len(equivalent) != 0 || !slices.Equal(branches, analyzers) {
		t.Fatalf("branches = %v, equivalent = %v", branches, equivalent)
	}
}

func TestAnalyzerEquivalenceRetainsUnavailableAndDictionaryAnalyzers(t *testing.T) {
	if !hasAnalyzerDictionaryTerm(
		cjkChineseTextAnalyzer,
		[]string{"搜索引擎"},
	) {
		t.Fatal("Chinese dictionary term was not classified")
	}
	unknownBranches, unknownEquivalent := requirementAnalyzerBranches(
		SearchRequest{Query: "query", Terms: []string{"query"}},
		[]string{"missing-analyzer"},
	)
	if !slices.Equal(unknownBranches, []string{"missing-analyzer"}) ||
		len(unknownEquivalent) != 0 {
		t.Fatalf(
			"unknown branches = %v, equivalent = %v",
			unknownBranches,
			unknownEquivalent,
		)
	}

	dictionaryBranches, dictionaryEquivalent := requirementAnalyzerBranches(
		SearchRequest{Query: "搜索引擎", Terms: []string{"搜索引擎"}},
		[]string{cjkChineseTextAnalyzer},
	)
	if !slices.Equal(dictionaryBranches, []string{cjkChineseTextAnalyzer}) ||
		len(dictionaryEquivalent) != 0 {
		t.Fatalf(
			"dictionary branches = %v, equivalent = %v",
			dictionaryBranches,
			dictionaryEquivalent,
		)
	}
}

func TestEquivalentAnalyzerPlanPreservesExactResultOrder(t *testing.T) {
	index := exactAnalyzerPlanIndex(t, 32)
	req := SearchRequest{
		Query:      "check point api",
		Terms:      []string{"check", "point", "api"},
		MaxResults: 32,
	}
	analyzers := queryAnalyzers(req.Query)
	full := searchAnalyzerPlan(t, index, uncollapsedRequiredTermsQuery(req, analyzers))
	collapsed := searchAnalyzerPlan(t, index, requiredTermsQuery(
		req,
		analyzers,
		req.Weights.orDefault(),
		true,
	))
	if full.Total != collapsed.Total || len(full.Hits) != len(collapsed.Hits) {
		t.Fatalf(
			"full = %d/%d, collapsed = %d/%d",
			full.Total,
			len(full.Hits),
			collapsed.Total,
			len(collapsed.Hits),
		)
	}
	for position := range full.Hits {
		if full.Hits[position].ID != collapsed.Hits[position].ID {
			t.Fatalf(
				"hit %d full = %s, collapsed = %s",
				position,
				full.Hits[position].ID,
				collapsed.Hits[position].ID,
			)
		}
	}
}

func TestEquivalentAnalyzerPlansPreserveMixedResultOrder(t *testing.T) {
	index := mixedAnalyzerPlanIndex(t)
	req := SearchRequest{
		Query:          "the best cats api guide",
		Terms:          []string{"the", "best", "cats", "api", "guide"},
		ExpansionTerms: []string{"reference"},
		MaxResults:     32,
	}
	analyzers := queryAnalyzers(req.Query)
	weights := req.Weights.orDefault()
	for _, test := range []struct {
		name      string
		full      blevequery.Query
		collapsed blevequery.Query
	}{
		{
			name:      "required",
			full:      uncollapsedRequiredTermsQuery(req, analyzers),
			collapsed: requiredTermsQuery(req, analyzers, weights, true),
		},
		{
			name: "minimum",
			full: func() blevequery.Query {
				request := req
				request.Relaxed = true
				return uncollapsedMinimumTermsQuery(request, analyzers)
			}(),
			collapsed: func() blevequery.Query {
				request := req
				request.Relaxed = true
				return minimumTermsQuery(request, analyzers, weights, true)
			}(),
		},
		{
			name: "fuzzy",
			full: func() blevequery.Query {
				request := req
				request.Query = "best cats api guide"
				request.Terms = []string{"best", "cats", "api", "guide"}
				request.ExpansionTerms = nil
				request.Fuzzy = true
				return uncollapsedFuzzyRecoveryQuery(request, analyzers)
			}(),
			collapsed: func() blevequery.Query {
				request := req
				request.Query = "best cats api guide"
				request.Terms = []string{"best", "cats", "api", "guide"}
				request.ExpansionTerms = nil
				request.Fuzzy = true
				return fuzzyRecoveryQuery(request, analyzers, weights, true)
			}(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			full := searchAnalyzerPlan(t, index, test.full)
			collapsed := searchAnalyzerPlan(t, index, test.collapsed)
			assertAnalyzerPlanOrder(t, full, collapsed)
		})
	}
}

func BenchmarkEquivalentAnalyzerQueryPlan(b *testing.B) {
	index := exactAnalyzerPlanIndex(b, 512)
	req := SearchRequest{
		Query:      "check point api",
		Terms:      []string{"check", "point", "api"},
		MaxResults: 50,
	}
	analyzers := queryAnalyzers(req.Query)
	for _, benchmark := range []struct {
		name  string
		query blevequery.Query
	}{
		{name: "uncollapsed", query: uncollapsedRequiredTermsQuery(req, analyzers)},
		{name: "collapsed", query: requiredTermsQuery(
			req,
			analyzers,
			req.Weights.orDefault(),
			true,
		)},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				result := searchAnalyzerPlan(b, index, benchmark.query)
				if result.Total != 512 {
					b.Fatalf("total = %d", result.Total)
				}
			}
		})
	}
}

func BenchmarkEquivalentAnalyzerStrictRelaxedCycle(b *testing.B) {
	index := exactAnalyzerPlanIndex(b, 512)
	req := SearchRequest{
		Query:      "check point api",
		Terms:      []string{"check", "point", "api"},
		MaxResults: 50,
	}
	relaxed := req
	relaxed.Relaxed = true
	analyzers := queryAnalyzers(req.Query)
	weights := req.Weights.orDefault()
	for _, benchmark := range []struct {
		name    string
		strict  blevequery.Query
		relaxed blevequery.Query
	}{
		{
			name:    "uncollapsed",
			strict:  uncollapsedRequiredTermsQuery(req, analyzers),
			relaxed: uncollapsedMinimumTermsQuery(relaxed, analyzers),
		},
		{
			name:    "collapsed",
			strict:  requiredTermsQuery(req, analyzers, weights, true),
			relaxed: minimumTermsQuery(relaxed, analyzers, weights, true),
		},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				strict := searchAnalyzerPlan(b, index, benchmark.strict)
				relaxed := searchAnalyzerPlan(b, index, benchmark.relaxed)
				if strict.Total != 512 || relaxed.Total != 512 {
					b.Fatalf("totals = %d/%d", strict.Total, relaxed.Total)
				}
			}
		})
	}
}

func exactAnalyzerPlanIndex(t testing.TB, documents int) *BleveMemoryIndex {
	t.Helper()
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := index.index.Close(); err != nil {
			t.Error(err)
		}
	})
	languages := []string{"en", "de", "fi", "tr"}
	for ordinal := range documents {
		err := index.Index(t.Context(), documentstore.Document{
			NormalizedURL: "https://example.test/" + strconv.Itoa(ordinal),
			Title:         "Check Point API",
			ExtractedText: "Check Point API reference and automation guide",
			Language:      languages[ordinal%len(languages)],
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	return index
}

func mixedAnalyzerPlanIndex(t testing.TB) *BleveMemoryIndex {
	t.Helper()
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := index.index.Close(); err != nil {
			t.Error(err)
		}
	})
	documents := []struct {
		id       string
		analyzer string
		title    string
		body     string
	}{
		{
			id:       "english-reference",
			analyzer: "en",
			title:    "Best cats API guide reference",
			body:     "the best cats api guide reference reference",
		},
		{
			id:       "english-guide",
			analyzer: "en",
			title:    "Best cats API guide",
			body:     "the best cats api integration guide",
		},
		{
			id:       "standard-reference",
			analyzer: standardTextAnalyzer,
			title:    "The best cats API guide",
			body:     "the best cats api guide reference",
		},
		{
			id:       "standard-guide",
			analyzer: standardTextAnalyzer,
			title:    "Best cats API guide",
			body:     "best cats api integration guide",
		},
		{
			id:       "german-reference",
			analyzer: "de",
			title:    "Best cats API guide reference",
			body:     "the best cats api guide reference",
		},
		{
			id:       "finnish-guide",
			analyzer: "fi",
			title:    "Best cats API guide",
			body:     "the best cats api guide",
		},
	}
	for _, document := range documents {
		if err := index.index.Index(document.id, bleveDocument{
			URL:      "https://example.test/" + document.id,
			Title:    document.title,
			Body:     document.body,
			Analyzer: document.analyzer,
		}); err != nil {
			t.Fatal(err)
		}
	}

	return index
}

func searchAnalyzerPlan(
	t testing.TB,
	index *BleveMemoryIndex,
	query blevequery.Query,
) *bleve.SearchResult {
	t.Helper()
	request := bleve.NewSearchRequest(query)
	request.Size = 50
	result, err := index.index.SearchInContext(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	return result
}

func uncollapsedRequiredTermsQuery(
	req SearchRequest,
	analyzers []string,
) blevequery.Query {
	weights := req.Weights.orDefault()
	strict := req
	strict.ExpansionTerms = nil
	branches := []blevequery.Query{strictRequiredTermsQuery(
		strict,
		[]string{standardTextAnalyzer},
		weights,
	)}
	for _, analyzer := range analyzers {
		terms := requirableTermsForAnalyzer(queryTermWords(req), analyzer)
		if len(terms) == 0 {
			continue
		}
		required := []blevequery.Query{analyzerScopeClause(analyzer)}
		for _, term := range terms {
			required = append(
				required,
				crossFieldTermClauseForAnalyzer(term, analyzer, weights),
			)
		}
		branches = append(branches, bleve.NewConjunctionQuery(required...))
	}

	main := bleve.NewDisjunctionQuery(branches...)
	if len(req.ExpansionTerms) == 0 {
		return main
	}
	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, term := range req.ExpansionTerms {
		query.AddShould(uncollapsedCrossFieldTermClause(
			term,
			analyzers,
			weights,
			expansionBoostFactor,
		))
	}

	return query
}

func uncollapsedMinimumTermsQuery(
	req SearchRequest,
	analyzers []string,
) blevequery.Query {
	weights := req.Weights.orDefault()
	branchRequest := req
	branchRequest.ExpansionTerms = nil
	branches := []blevequery.Query{strictMinimumTermsQuery(
		branchRequest,
		[]string{standardTextAnalyzer},
		weights,
	)}
	for _, analyzer := range analyzers {
		terms := requirableTermsForAnalyzer(queryTermWords(req), analyzer)
		if len(terms) == 0 {
			continue
		}
		minimum := minimumTermRequirement(req, len(terms))
		matches := queryWithMinimumTerms(terms, minimum, func(term string) blevequery.Query {
			return crossFieldTermClauseForAnalyzer(term, analyzer, weights)
		})
		branches = append(branches, bleve.NewConjunctionQuery(
			analyzerScopeClause(analyzer),
			matches,
		))
	}
	main := bleve.NewDisjunctionQuery(branches...)
	if len(req.ExpansionTerms) == 0 {
		return main
	}
	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, term := range req.ExpansionTerms {
		query.AddShould(uncollapsedCrossFieldTermClause(
			term,
			analyzers,
			weights,
			expansionBoostFactor,
		))
	}

	return query
}

func uncollapsedFuzzyRecoveryQuery(
	req SearchRequest,
	analyzers []string,
) blevequery.Query {
	weights := req.Weights.orDefault()
	branches := []blevequery.Query{strictFuzzyRecoveryQuery(
		req,
		[]string{standardTextAnalyzer},
		weights,
	)}
	for _, analyzer := range analyzers {
		terms := requirableTermsForAnalyzer(queryTermWords(req), analyzer)
		if len(terms) == 0 {
			continue
		}
		required := []blevequery.Query{analyzerScopeClause(analyzer)}
		for _, term := range terms {
			required = append(
				required,
				fuzzyCrossFieldTermClauseForAnalyzer(term, analyzer, weights),
			)
		}
		branches = append(branches, bleve.NewConjunctionQuery(required...))
	}

	return bleve.NewDisjunctionQuery(branches...)
}

func uncollapsedCrossFieldTermClause(
	text string,
	analyzers []string,
	weights RankingWeights,
	factor float64,
) blevequery.Query {
	clause := bleve.NewDisjunctionQuery()
	for _, analyzer := range analyzers {
		for _, field := range textSearchFields() {
			clause.AddQuery(fieldMatchWithAnalyzer(
				field,
				text,
				textFieldWeight(field, weights)*factor,
				analyzer,
			))
		}
	}
	clause.AddQuery(fieldMatch("url", text, weights.URL*factor))

	return clause
}

func assertAnalyzerPlanOrder(
	t testing.TB,
	full *bleve.SearchResult,
	collapsed *bleve.SearchResult,
) {
	t.Helper()
	if full.Total != collapsed.Total || len(full.Hits) != len(collapsed.Hits) {
		t.Fatalf(
			"full = %d/%d, collapsed = %d/%d",
			full.Total,
			len(full.Hits),
			collapsed.Total,
			len(collapsed.Hits),
		)
	}
	scoreScale := 0.0
	for position := range full.Hits {
		if full.Hits[position].ID != collapsed.Hits[position].ID {
			t.Fatalf(
				"hit %d full = %s (%f), collapsed = %s (%f)",
				position,
				full.Hits[position].ID,
				full.Hits[position].Score,
				collapsed.Hits[position].ID,
				collapsed.Hits[position].Score,
			)
		}
		if full.Hits[position].Score == 0 {
			if collapsed.Hits[position].Score != 0 {
				t.Fatalf(
					"hit %d score full = %f, collapsed = %f",
					position,
					full.Hits[position].Score,
					collapsed.Hits[position].Score,
				)
			}
			continue
		}
		ratio := collapsed.Hits[position].Score / full.Hits[position].Score
		if scoreScale == 0 {
			scoreScale = ratio
		}
		if math.Abs(ratio-scoreScale) > 1e-12*max(1, math.Abs(scoreScale)) {
			t.Fatalf(
				"hit %d score ratio = %f, want %f",
				position,
				ratio,
				scoreScale,
			)
		}
	}
}
