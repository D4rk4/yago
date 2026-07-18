package searchremote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagonode/internal/searchvisible"
	"github.com/D4rk4/yago/yagoproto"
)

func TestRemoteSearcherNegotiatesEvidenceOnPrimaryAndSecondaryRequests(t *testing.T) {
	fixture := newMultiTermAbstractFixture(t)
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	_, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{"alpha", "beta"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	requests := fixture.recordedRequests()
	if len(requests) != 4 {
		t.Fatalf("request count = %d", len(requests))
	}
	for _, request := range requests {
		version := request.Get(yagoproto.FieldQueryEvidenceVersion)
		terms := request[yagoproto.FieldQueryEvidenceTerm]
		if request.Get(yagoproto.FieldAbstracts) != "" {
			if version != "" || terms != nil {
				t.Fatalf("abstract request negotiated evidence: %v", request)
			}
			continue
		}
		if version != "1" || !reflect.DeepEqual(terms, []string{"alpha", "beta"}) {
			t.Fatalf("resource request evidence version=%q terms=%q", version, terms)
		}
	}
}

func TestMorphologyVariantsBindEvidenceToEachWireRequirement(t *testing.T) {
	type variantEvidenceRequest struct {
		query string
		terms []string
	}
	requests := make(chan variantEvidenceRequest, 4)
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			form := request.URL.Query()
			requests <- variantEvidenceRequest{
				query: form.Get(yagoproto.FieldQuery),
				terms: slices.Clone(form[yagoproto.FieldQueryEvidenceTerm]),
			}
			writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
		}),
	)
	defer server.Close()
	_, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
		ExpandWord: func(word string) []string {
			return []string{word, "полномочий", "полномочиями"}
		},
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"полномочия"},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	close(requests)
	seen := make(map[string]int)
	for request := range requests {
		if len(request.terms) != 1 ||
			request.query != yagomodel.WordHash(request.terms[0]).String() {
			t.Fatalf("variant request = %#v", request)
		}
		seen[request.terms[0]]++
	}
	for _, expected := range []string{"полномочия", "полномочий", "полномочиями"} {
		if seen[expected] != 1 {
			t.Fatalf("variant %q requests = %d; all=%v", expected, seen[expected], seen)
		}
	}
}

func TestRemoteConversionAppliesValidatedQueryMatchEvidence(t *testing.T) {
	hash := hashFor("evidence")
	row := metadataRow(t, hash, "https://example.test/report", "Metadata title")
	evidence := validRemoteQueryMatchEvidence()
	requirements := []string{"чрезвычайные", "полномочия"}
	results, err := searchResultsWithEvidenceWithinBudget(
		t.Context(),
		evidenceSearchResults{
			request:  searchcore.Request{Terms: requirements, Limit: 10},
			rows:     []yagomodel.URIMetadataRow{row},
			evidence: map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: evidence},
			scorer:   newRemoteScorer(requirements, DefaultRankingWeights()),
			budget:   newRemoteQueryBudget(),
		},
	)
	if err != nil {
		t.Fatalf("searchResultsWithEvidenceWithinBudget: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results[0]
	if !result.EvidenceReady || result.Analyzer != "ru" ||
		result.Snippet != evidence.Snippet || len(result.QueryMatches) != 2 ||
		len(result.BodyQueryMatches) != 2 ||
		!reflect.DeepEqual(result.FieldTermPositions["body"]["чрезвычайные"], []int{12}) ||
		!reflect.DeepEqual(result.FieldTermPositions["body"]["полномочия"], []int{13}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestVariantResponseRemapsOnlyLocallyBoundRequirement(t *testing.T) {
	hash := hashFor("evidence")
	row := metadataRow(t, hash, "https://example.test/report", "Metadata title")
	item := validRemoteQueryMatchEvidence()
	item.Snippet = "полномочий"
	item.SnippetMatches = item.SnippetMatches[:1]
	item.SnippetMatches[0] = yagoproto.QueryMatchRange{Start: 0, End: len(item.Snippet)}
	item.BodyMatches = item.BodyMatches[:1]
	item.FieldPositions[0].Requirements = item.FieldPositions[0].Requirements[:1]
	item.RequirementOrdinals = []int{0}
	request := remoteSearchRequest(
		searchcore.Request{Terms: []string{"полномочий"}},
		"freeworld",
		time.Second,
	)
	binding := singleWordMorphologyQueryMatchEvidenceBinding("полномочий", "полномочия")
	binding.request(&request)
	response := (searcher{weights: DefaultRankingWeights}).response(
		t.Context(),
		searchcore.Request{Terms: []string{"полномочий"}, Limit: 10},
		[]peerSearchResult{{
			peer:            yagomodel.Seed{Hash: hashFor("peer")},
			evidenceBinding: binding.negotiated(request),
			response: yagoproto.SearchResponse{
				Count:     1,
				Resources: []yagomodel.URIMetadataRow{row},
				ResourceEvidence: map[yagomodel.Hash]yagoproto.QueryMatchEvidence{
					hash: item,
				},
			},
		}},
		nil,
	)
	if len(response.Results) != 1 || !response.Results[0].EvidenceReady ||
		len(response.Results[0].FieldTermPositions["body"]["полномочия"]) == 0 ||
		len(response.Results[0].FieldTermPositions["body"]["полномочий"]) != 0 {
		t.Fatalf("variant response = %#v", response)
	}
}

func TestVariantEvidenceBindingRejectsWireHashMismatch(t *testing.T) {
	binding := singleWordMorphologyQueryMatchEvidenceBinding("полномочий", "полномочия")
	request := yagoproto.SearchRequest{
		Query: []yagomodel.Hash{yagomodel.WordHash("полномочия")},
	}
	binding.request(&request)
	if negotiated := binding.negotiated(request); negotiated.valid() {
		t.Fatalf("mismatched binding negotiated = %#v", negotiated)
	}
	invalid := []queryMatchEvidenceBinding{
		{
			wireRequirements:    []string{"полномочий"},
			rankingRequirements: []string{"полномочия"},
		},
		{
			wireRequirements:       []string{"полномочий", "чрезвычайных"},
			rankingRequirements:    []string{"полномочия", "чрезвычайные"},
			singleRequirementRemap: true,
		},
		{
			wireRequirements:       []string{"полномочий"},
			rankingRequirements:    []string{"полномочия", "чрезвычайные"},
			singleRequirementRemap: true,
		},
	}
	for index, candidate := range invalid {
		if candidate.valid() {
			t.Fatalf("invalid binding %d accepted = %#v", index, candidate)
		}
	}
}

func TestSecondaryEvidenceBindingRejectsOutOfAllowlistResource(t *testing.T) {
	allowed := hashFor("allowed")
	disallowed := hashFor("disallowed")
	request := yagoproto.SearchRequest{
		Query: []yagomodel.Hash{yagomodel.WordHash("term")},
		URLs:  []yagomodel.Hash{allowed},
	}
	binding := identityQueryMatchEvidenceBinding([]string{"term"})
	binding.request(&request)
	binding = binding.negotiated(request)
	item := yagoproto.QueryMatchEvidence{
		Version:             yagoproto.QueryMatchEvidenceVersion,
		Analyzer:            "en",
		RequirementOrdinals: []int{0},
		AbsentOrdinals:      []int{},
		FieldPositions: []yagoproto.QueryFieldPositions{{
			Field: "body",
			Requirements: []yagoproto.QueryRequirementPositions{{
				Ordinal:   0,
				Positions: []int{1},
			}},
		}},
	}
	result := searchcore.Result{
		URLHash:  disallowed.String(),
		URL:      "https://example.test/disallowed",
		Title:    "term",
		Language: "en",
	}
	applied := resultWithQueryMatchEvidenceBinding(
		binding,
		result,
		map[yagomodel.Hash]yagoproto.QueryMatchEvidence{disallowed: item},
	)
	if applied.EvidenceReady {
		t.Fatalf("out-of-allowlist evidence applied = %#v", applied)
	}
	result.URLHash = allowed.String()
	applied = resultWithQueryMatchEvidenceBinding(
		binding,
		result,
		map[yagomodel.Hash]yagoproto.QueryMatchEvidence{allowed: item},
	)
	if !applied.EvidenceReady {
		t.Fatalf("allowlisted evidence rejected = %#v", applied)
	}
}

func TestRemoteConversionRejectsMalformedOrMismatchedEvidence(t *testing.T) {
	hash := hashFor("evidence")
	row := metadataRow(t, hash, "https://example.test/report", "Metadata title")
	requirements := []string{"чрезвычайные", "полномочия"}
	mutations := malformedRemoteEvidenceMutations()
	for index, mutate := range mutations {
		item := validRemoteQueryMatchEvidence()
		mutate(&item)
		results, err := searchResultsWithEvidenceWithinBudget(
			t.Context(),
			evidenceSearchResults{
				request:  searchcore.Request{Terms: requirements, Limit: 10},
				rows:     []yagomodel.URIMetadataRow{row},
				evidence: map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: item},
				scorer:   newRemoteScorer(nil, DefaultRankingWeights()),
				budget:   newRemoteQueryBudget(),
			},
		)
		if err != nil || len(results) != 1 || results[0].EvidenceReady {
			t.Fatalf("case %d result=%#v err=%v", index, results, err)
		}
	}
	assertAbsentRemoteEvidence(t, row)
	assertInvalidRemoteEvidenceRequirements(t, hash)
}

func malformedRemoteEvidenceMutations() []func(*yagoproto.QueryMatchEvidence) {
	return []func(*yagoproto.QueryMatchEvidence){
		func(item *yagoproto.QueryMatchEvidence) { item.Version = 2 },
		func(item *yagoproto.QueryMatchEvidence) { item.Analyzer = "missing" },
		func(item *yagoproto.QueryMatchEvidence) { item.Analyzer = "en" },
		func(item *yagoproto.QueryMatchEvidence) { item.Snippet = "bad\xff" },
		func(item *yagoproto.QueryMatchEvidence) { item.SnippetMatches[0].Start = 1 },
		func(item *yagoproto.QueryMatchEvidence) { item.BodyMatches[0].Start = -1 },
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Ordinal = 2
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Positions = []int{2, 1}
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions = append(item.FieldPositions, item.FieldPositions[0])
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions[0].Field = "unknown"
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions[0].Requirements = make(
				[]yagoproto.QueryRequirementPositions,
				maximumAppliedEvidenceRequirements+1,
			)
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions[0].Requirements = append(
				item.FieldPositions[0].Requirements,
				item.FieldPositions[0].Requirements[0],
			)
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Positions = sequentialRemotePositions(
				maximumAppliedRequirementPositions + 1,
			)
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions = maximumRemoteEvidenceFieldsWithPositions()
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions = make(
				[]yagoproto.QueryFieldPositions,
				maximumAppliedEvidenceFields+1,
			)
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.Snippet = strings.Repeat("x", maximumAppliedEvidenceSnippetBytes+1)
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.SnippetMatches = make(
				[]yagoproto.QueryMatchRange,
				maximumAppliedEvidenceMatches+1,
			)
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.BodyMatches = make(
				[]yagoproto.QueryMatchRange,
				maximumAppliedEvidenceMatches+1,
			)
		},
	}
}

func assertAbsentRemoteEvidence(
	t *testing.T,
	row yagomodel.URIMetadataRow,
) {
	t.Helper()
	requirements := []string{"чрезвычайные", "полномочия"}
	results, err := searchResultsWithEvidenceWithinBudget(
		t.Context(),
		evidenceSearchResults{
			request: searchcore.Request{Terms: requirements, Limit: 10},
			rows:    []yagomodel.URIMetadataRow{row},
			scorer:  newRemoteScorer(nil, DefaultRankingWeights()),
			budget:  newRemoteQueryBudget(),
		},
	)
	if err != nil || len(results) != 1 || results[0].EvidenceReady {
		t.Fatalf("absent evidence result=%#v err=%v", results, err)
	}
}

func assertInvalidRemoteEvidenceRequirements(t *testing.T, hash yagomodel.Hash) {
	t.Helper()
	item := validRemoteQueryMatchEvidence()
	for _, requirements := range [][]string{
		nil,
		{"", "полномочия"},
		make([]string, maximumAppliedEvidenceRequirements+1),
	} {
		result := resultWithQueryMatchEvidence(
			requirements,
			searchcore.Result{URLHash: hash.String(), Title: "Чрезвычайных полномочий"},
			map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: item},
		)
		if result.EvidenceReady {
			t.Fatalf("invalid requirements applied: %q", requirements)
		}
	}
}

func TestRemoteEvidenceSupportsEmptySnippetAndDuplicateRequirementSurfaces(t *testing.T) {
	hash := hashFor("evidence")
	item := yagoproto.QueryMatchEvidence{
		Version:             yagoproto.QueryMatchEvidenceVersion,
		Analyzer:            "en",
		RequirementOrdinals: []int{0},
		AbsentOrdinals:      []int{},
		FieldPositions: []yagoproto.QueryFieldPositions{{
			Field: "body",
			Requirements: []yagoproto.QueryRequirementPositions{{
				Ordinal: 0, Positions: []int{1, 2},
			}},
		}},
	}
	result := resultWithQueryMatchEvidence(
		[]string{"metadata", "metadata"},
		searchcore.Result{
			URLHash:  hash.String(),
			URL:      "https://example.test/metadata",
			Title:    "Metadata title",
			Snippet:  "existing snippet",
			Language: "en",
		},
		map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: item},
	)
	if !result.EvidenceReady || result.Snippet != "existing snippet" ||
		!reflect.DeepEqual(result.FieldTermPositions["body"]["metadata"], []int{1, 2}) {
		t.Fatalf("result = %#v", result)
	}
	if result.QueryMatches == nil || len(result.QueryMatches) != 0 {
		t.Fatalf("authoritative empty snippet matches = %#v", result.QueryMatches)
	}
	if got := queryMatchEvidenceTerms(
		searchcore.Request{Query: "alpha beta"},
	); !reflect.DeepEqual(
		got,
		[]string{"alpha", "beta"},
	) {
		t.Fatalf("query evidence terms = %q", got)
	}
}

func TestMalformedRemoteEvidenceLeavesVisibleAnalyzerFallbackAvailable(t *testing.T) {
	hash := hashFor("evidence")
	item := validRemoteQueryMatchEvidence()
	item.Analyzer = "en"
	result := resultWithQueryMatchEvidence(
		[]string{"чрезвычайные", "полномочия"},
		searchcore.Result{
			URLHash:  hash.String(),
			URL:      "https://example.test/report",
			Title:    "Чрезвычайных полномочий",
			Snippet:  "Чрезвычайных полномочий передали президенту",
			Language: "ru",
		},
		map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: item},
	)
	if result.EvidenceReady {
		t.Fatal("incompatible evidence suppressed fallback")
	}
	response, err := searchvisible.NewVisibleEvidenceSearcher(
		queryEvidenceResultSource{result: result},
	).Search(t.Context(), searchcore.Request{
		Terms: []string{"чрезвычайные", "полномочия"},
	})
	if err != nil || len(response.Results) != 1 || !response.Results[0].EvidenceReady ||
		response.Results[0].Analyzer != "ru" ||
		len(response.Results[0].FieldTermPositions["snippet"]["чрезвычайные"]) == 0 ||
		len(response.Results[0].FieldTermPositions["snippet"]["полномочия"]) == 0 {
		t.Fatalf("fallback response=%#v err=%v", response, err)
	}
}

func TestResourceBudgetRetainsOnlyDetachedEvidenceForRetainedRows(t *testing.T) {
	firstHash := hashFor("first")
	secondHash := hashFor("second")
	first := metadataRow(t, firstHash, "https://example.test/first", "First")
	second := metadataRow(t, secondHash, "https://example.test/second", "Second")
	item := validRemoteQueryMatchEvidence()
	response := retainedResourceResponse(yagoproto.SearchResponse{
		Count:     2,
		Resources: []yagomodel.URIMetadataRow{first, second},
		ResourceEvidence: map[yagomodel.Hash]yagoproto.QueryMatchEvidence{
			firstHash:  item,
			secondHash: item,
		},
	}, 1)
	if len(response.Resources) != 1 || len(response.ResourceEvidence) != 1 {
		t.Fatalf("retained response = %#v", response)
	}
	retained := response.ResourceEvidence[firstHash]
	item.SnippetMatches[0].Start = 9
	item.BodyMatches[0].Start = 9
	item.FieldPositions[0].Requirements[0].Positions[0] = 99
	item.RequirementOrdinals[0] = 9
	item.AbsentOrdinals = append(item.AbsentOrdinals, 9)
	if retained.SnippetMatches[0].Start == 9 || retained.BodyMatches[0].Start == 9 ||
		retained.FieldPositions[0].Requirements[0].Positions[0] == 99 ||
		retained.RequirementOrdinals[0] == 9 || len(retained.AbsentOrdinals) != 0 {
		t.Fatalf("retained evidence aliases source: %#v", retained)
	}
	if retainedQueryMatchEvidence(nil, []yagomodel.URIMetadataRow{first}) != nil ||
		retainedQueryMatchEvidence(
			map[yagomodel.Hash]yagoproto.QueryMatchEvidence{firstHash: item},
			[]yagomodel.URIMetadataRow{{Properties: map[string]string{"hash": "bad"}}},
		) != nil ||
		retainedQueryMatchEvidence(
			map[yagomodel.Hash]yagoproto.QueryMatchEvidence{secondHash: item},
			[]yagomodel.URIMetadataRow{first},
		) != nil {
		t.Fatal("empty retained evidence must stay nil")
	}
}

type remoteEvidenceSessionSource struct {
	results []searchcore.Result
}

func (s *remoteEvidenceSessionSource) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{TotalResults: len(s.results), Results: s.results}, nil
}

func TestNegotiatedEvidenceSurvivesFinalPayloadAndLaterSessionPage(t *testing.T) {
	requirements := []string{"чрезвычайные", "полномочия"}
	hash := hashFor("evidence")
	item := validRemoteQueryMatchEvidence()
	remote := resultWithQueryMatchEvidence(
		requirements,
		searchcore.Result{
			URLHash:  hash.String(),
			URL:      "https://target.example/report",
			Title:    "Чрезвычайные полномочия",
			Snippet:  "legacy snippet",
			Language: "ru",
			Source:   searchcore.SourceRemote,
			Score:    85,
		},
		map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: item},
	)
	if !remote.EvidenceReady || len(remote.QueryMatches) != 2 ||
		len(remote.BodyQueryMatches) != 2 || len(remote.FieldTermPositions) == 0 {
		t.Fatalf("applied remote evidence = %#v", remote)
	}
	results := make([]searchcore.Result, 20)
	for index := range results {
		results[index] = searchcore.Result{
			URL:   "https://host-" + strconv.Itoa(index) + ".example/result",
			Score: float64(100 - index),
		}
	}
	results[15] = remote
	source := &remoteEvidenceSessionSource{results: results}
	stable := searchsession.NewStableWindow(searchcore.NewFinalRankingSearcher(source))
	request := searchcore.Request{
		Query: strings.Join(requirements, " "),
		Terms: requirements,
		Limit: 10,
	}
	first, err := stable.Search(t.Context(), request)
	if err != nil || len(first.Results) != 10 {
		t.Fatalf("first page=%#v error=%v", first, err)
	}
	source.results[15].Snippet = "mutated"
	source.results[15].QueryMatches[0].Start = 99
	source.results[15].BodyQueryMatches[0].Start = 99
	source.results[15].FieldTermPositions["body"]["чрезвычайные"][0] = 99
	request.Offset = 10
	second, err := stable.Search(t.Context(), request)
	if err != nil || len(second.Results) != 10 {
		t.Fatalf("second page=%#v error=%v", second, err)
	}
	var retained *searchcore.Result
	for index := range second.Results {
		if second.Results[index].URL == remote.URL {
			retained = &second.Results[index]
			break
		}
	}
	if retained == nil || retained.Snippet != item.Snippet ||
		retained.QueryMatches[0].Start != item.SnippetMatches[0].Start ||
		retained.BodyQueryMatches[0].Start != item.BodyMatches[0].Start ||
		retained.FieldTermPositions != nil {
		t.Fatalf("retained later-page evidence = %#v", retained)
	}
}

func BenchmarkRemoteQueryMatchEvidenceApplication(b *testing.B) {
	requirements := []string{"чрезвычайные", "полномочия"}
	item := validRemoteQueryMatchEvidence()
	hash := hashFor("evidence")
	evidence := map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: item}
	result := searchcore.Result{URLHash: hash.String(), Snippet: "fallback"}
	b.ReportAllocs()
	for b.Loop() {
		applied := resultWithQueryMatchEvidence(requirements, result, evidence)
		if !applied.EvidenceReady {
			b.Fatal("evidence not applied")
		}
	}
}

func validRemoteQueryMatchEvidence() yagoproto.QueryMatchEvidence {
	snippet := "чрезвычайных полномочий"
	second := strings.Index(snippet, "полномочий")

	return yagoproto.QueryMatchEvidence{
		Version:             yagoproto.QueryMatchEvidenceVersion,
		Analyzer:            "ru",
		RequirementOrdinals: []int{0, 1},
		AbsentOrdinals:      []int{},
		Snippet:             snippet,
		SnippetMatches: []yagoproto.QueryMatchRange{
			{Start: 0, End: len("чрезвычайных")},
			{Start: second, End: len(snippet)},
		},
		BodyMatches: []yagoproto.QueryMatchRange{
			{Start: 40, End: 54},
			{Start: 55, End: 66},
		},
		FieldPositions: []yagoproto.QueryFieldPositions{{
			Field: "body",
			Requirements: []yagoproto.QueryRequirementPositions{
				{Ordinal: 0, Positions: []int{12}},
				{Ordinal: 1, Positions: []int{13}},
			},
		}},
	}
}

func sequentialRemotePositions(total int) []int {
	positions := make([]int, total)
	for index := range positions {
		positions[index] = index + 1
	}

	return positions
}

func maximumRemoteEvidenceFieldsWithPositions() []yagoproto.QueryFieldPositions {
	fields := []string{"title", "headings", "anchors", "body", "url"}
	mapped := make([]yagoproto.QueryFieldPositions, len(fields))
	for index, field := range fields {
		mapped[index] = yagoproto.QueryFieldPositions{
			Field: field,
			Requirements: []yagoproto.QueryRequirementPositions{{
				Ordinal:   0,
				Positions: sequentialRemotePositions(maximumAppliedRequirementPositions),
			}},
		}
	}

	return mapped
}

type queryEvidenceResultSource struct {
	result searchcore.Result
}

func (s queryEvidenceResultSource) Search(
	_ context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{
		Request:      request,
		TotalResults: 1,
		Results:      []searchcore.Result{s.result},
	}, nil
}
