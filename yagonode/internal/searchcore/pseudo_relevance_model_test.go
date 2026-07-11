package searchcore

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPseudoRelevanceModelInterpolatesQueryAndFeedbackProbabilities(t *testing.T) {
	results := []Result{
		{URL: "one", Score: 2, Title: "Alpha", Snippet: "bridge"},
		{URL: "two", Score: 2, Title: "alpha", Snippet: "bridge"},
	}
	model := buildPseudoRelevanceModel(results, []string{"ALPHA alpha", "beta"})
	assertNear(t, model["alpha"].feedbackProbability, 0.5)
	assertNear(t, model["alpha"].probability, 7.0/12.0)
	assertNear(t, model["bridge"].probability, 0.25)
	assertNear(t, model["beta"].probability, 1.0/6.0)
	if model["alpha"].documents != 2 || model["bridge"].documents != 2 ||
		model["beta"].documents != 0 {
		t.Fatalf("model support = %#v", model)
	}
	total := 0.0
	for _, evidence := range model {
		total += evidence.probability
	}
	assertNear(t, total, 1)

	feedbackOnly := buildPseudoRelevanceModel(results, nil)
	assertNear(t, feedbackOnly["alpha"].probability, 0.5)
	assertNear(t, feedbackOnly["bridge"].probability, 0.5)
	if model := buildPseudoRelevanceModel(results[:1], nil); model != nil {
		t.Fatalf("one-document model = %#v", model)
	}
	duplicates := []Result{
		{URL: "same", Snippet: "central"},
		{URL: "same", Snippet: "central"},
	}
	if model := buildPseudoRelevanceModel(duplicates, nil); model != nil {
		t.Fatalf("duplicate-document model = %#v", model)
	}
}

func TestPseudoRelevanceDocumentPosteriorsNormalizeScoresAndRanks(t *testing.T) {
	documents := []pseudoRelevanceDocument{
		{rank: 1, score: 0},
		{rank: 2, score: 10},
		{rank: 3, score: -10},
		{rank: 4, score: math.NaN()},
		{rank: 5, score: math.Inf(1)},
	}
	posteriors := pseudoRelevanceDocumentPosteriors(documents)
	want := []float64{90.0 / 197.0, 60.0 / 197.0, 20.0 / 197.0, 15.0 / 197.0, 12.0 / 197.0}
	for index := range want {
		assertNear(t, posteriors[index], want[index])
	}
	assertNear(t, sumProbabilities(posteriors), 1)

	equal := pseudoRelevanceDocumentPosteriors([]pseudoRelevanceDocument{
		{rank: 1, score: 4},
		{rank: 2, score: 4},
	})
	assertNear(t, equal[0], 2.0/3.0)
	assertNear(t, equal[1], 1.0/3.0)

	invalid := pseudoRelevanceDocumentPosteriors([]pseudoRelevanceDocument{
		{rank: 1, score: math.NaN()},
		{rank: 2, score: math.Inf(-1)},
	})
	assertNear(t, invalid[0], 2.0/3.0)
	assertNear(t, invalid[1], 1.0/3.0)
	if got := pseudoRelevanceDocumentPosteriors(nil); len(got) != 0 {
		t.Fatalf("empty posteriors = %v", got)
	}
}

func TestMinePseudoRelevanceTermsFiltersDriftAndUsesDocumentLength(t *testing.T) {
	filtered := []Result{
		{URL: "one", Snippet: "Alpha forbidden the cat central"},
		{URL: "two", Snippet: "alpha forbidden the cat central"},
	}
	if got := minePseudoRelevanceTerms(
		filtered,
		[]string{"alpha"},
		[]string{"forbidden"},
	); !reflect.DeepEqual(got, []string{"central"}) {
		t.Fatalf("filtered expansion = %v", got)
	}

	longDocument := strings.TrimSpace(
		strings.Repeat("verbose ", 5) + strings.Repeat("the ", 95),
	)
	lengthNormalized := []Result{
		{URL: "one", Snippet: "concise cat the"},
		{URL: "two", Snippet: "concise cat the"},
		{URL: "three", Snippet: "the"},
		{URL: "four", Snippet: longDocument},
		{URL: "five", Snippet: longDocument},
	}
	if got := minePseudoRelevanceTerms(lengthNormalized, nil, nil); !reflect.DeepEqual(
		got,
		[]string{"concise", "verbose"},
	) {
		t.Fatalf("length-normalized expansion = %v", got)
	}

	duplicatePage := []Result{
		{URL: "one", ClusterID: "same", Snippet: "central central"},
		{URL: "two", ClusterID: "same", Snippet: "central central"},
	}
	if got := minePseudoRelevanceTerms(duplicatePage, nil, nil); len(got) != 0 {
		t.Fatalf("duplicate-page expansion = %v", got)
	}

	onePage := []Result{
		{URL: "one", Snippet: "central central central"},
		{URL: "two", Snippet: "different evidence"},
	}
	if got := minePseudoRelevanceTerms(onePage, nil, nil); len(got) != 0 {
		t.Fatalf("one-page expansion = %v", got)
	}
}

func TestPseudoRelevanceBoundsDocumentsTokensAndExpansionTerms(t *testing.T) {
	results := []Result{
		{URL: "one", Snippet: "lateword"},
		{URL: "two", Snippet: "second"},
		{URL: "three", Snippet: "third"},
		{URL: "four", Snippet: "fourth"},
		{URL: "five", Snippet: "fifth"},
		{URL: "six", Snippet: "lateword"},
	}
	if got := minePseudoRelevanceTerms(results, nil, nil); len(got) != 0 {
		t.Fatalf("term from sixth document expanded = %v", got)
	}

	cappedText := strings.Repeat("the ", prfMaximumDocumentTokens)
	capped := []Result{
		{URL: "one", Title: cappedText, Snippet: "lateword"},
		{URL: "two", Title: cappedText, Snippet: "lateword"},
	}
	if got := minePseudoRelevanceTerms(capped, nil, nil); len(got) != 0 {
		t.Fatalf("term after token cap expanded = %v", got)
	}

	crowded := []Result{
		{URL: "one", Snippet: "delta bravo echo alpha charlie"},
		{URL: "two", Snippet: "delta bravo echo alpha charlie"},
	}
	if got := minePseudoRelevanceTerms(crowded, nil, nil); !reflect.DeepEqual(
		got,
		[]string{"alpha", "bravo", "charlie"},
	) {
		t.Fatalf("bounded expansion = %v", got)
	}
}

func TestPseudoRelevanceDocumentSelectionAndTokenization(t *testing.T) {
	title := strings.Repeat("title ", prfMaximumDocumentTokens)
	results := []Result{
		{URL: "one", Title: title, Snippet: "ignored"},
		{URL: "one", Snippet: "duplicate"},
		{URL: "two"},
		{URL: "three", Snippet: "third"},
		{URL: "four", Snippet: "fourth"},
		{URL: "six", Snippet: "outside"},
	}
	documents := pseudoRelevanceDocuments(results)
	if len(documents) != 3 || documents[0].rank != 1 || documents[1].rank != 4 ||
		documents[2].rank != 5 {
		t.Fatalf("feedback documents = %#v", documents)
	}
	if documents[0].length != prfMaximumDocumentTokens ||
		documents[0].termFrequency["title"] != prfMaximumDocumentTokens ||
		documents[0].termFrequency["ignored"] != 0 {
		t.Fatalf("bounded document = %#v", documents[0])
	}
	if got := pseudoRelevanceDocuments(nil); len(got) != 0 {
		t.Fatalf("empty documents = %#v", got)
	}

	exact := strings.Repeat("a", prfMaximumTermRunes)
	overlong := strings.Repeat("b", prfMaximumTermRunes+2)
	tokens := pseudoRelevanceTokens(
		"\u0301 CAFÉ42 cafe\u0301 "+exact+" "+overlong+" tail",
		10,
	)
	want := []string{"café42", "cafe\u0301", exact, "tail"}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tokens = %#v, want %#v", tokens, want)
	}
	if got := pseudoRelevanceTokens("one two", 1); !reflect.DeepEqual(got, []string{"one"}) {
		t.Fatalf("limited tokens = %v", got)
	}
	if got := pseudoRelevanceTokens("one", 0); got != nil {
		t.Fatalf("zero-limit tokens = %v", got)
	}
}

func TestPseudoRelevanceQueryVocabularyAndCandidateOrdering(t *testing.T) {
	requestTerms := []string{"Alpha", "Beta"}
	if got := pseudoRelevanceQueryTerms(
		Request{Terms: requestTerms, Query: "ignored"},
	); !reflect.DeepEqual(got, requestTerms) {
		t.Fatalf("request terms = %v", got)
	}
	if got := pseudoRelevanceQueryTerms(
		Request{Query: "raw query"},
	); !reflect.DeepEqual(
		got,
		[]string{"raw query"},
	) {
		t.Fatalf("raw query terms = %v", got)
	}
	if got := pseudoRelevanceTermTokens(
		[]string{"One two", "THREE"},
		2,
	); !reflect.DeepEqual(
		got,
		[]string{"one", "two"},
	) {
		t.Fatalf("term tokens = %v", got)
	}
	if got := pseudoRelevanceTermTokens([]string{"one"}, 0); got != nil {
		t.Fatalf("zero-limit term tokens = %v", got)
	}
	blocked := pseudoRelevanceBlockedTerms(
		[]string{"Alpha beta"},
		[]string{"Gamma"},
	)
	if !blocked["alpha"] || !blocked["beta"] || !blocked["gamma"] || len(blocked) != 3 {
		t.Fatalf("blocked terms = %v", blocked)
	}
	frequency := pseudoRelevanceTermFrequency([]string{"alpha", "beta", "alpha"})
	if frequency["alpha"] != 2 || frequency["beta"] != 1 {
		t.Fatalf("term frequency = %v", frequency)
	}

	if !pseudoRelevanceCandidateLess(
		pseudoRelevanceCandidate{term: "z", probability: 2, documents: 2},
		pseudoRelevanceCandidate{term: "a", probability: 1, documents: 3},
	) {
		t.Fatalf("probability order failed")
	}
	if !pseudoRelevanceCandidateLess(
		pseudoRelevanceCandidate{term: "z", probability: 1, documents: 3},
		pseudoRelevanceCandidate{term: "a", probability: 1, documents: 2},
	) {
		t.Fatalf("document support order failed")
	}
	if !pseudoRelevanceCandidateLess(
		pseudoRelevanceCandidate{term: "a", probability: 1, documents: 2},
		pseudoRelevanceCandidate{term: "z", probability: 1, documents: 2},
	) {
		t.Fatalf("term order failed")
	}
}

func TestPseudoRelevanceSecondPassPreservesRequestControls(t *testing.T) {
	first := Response{Results: []Result{
		{URL: "one", Snippet: "alpha central"},
		{URL: "two", Snippet: "alpha central"},
	}}
	inner := &scriptedSearcher{responses: []Response{first, {}}}
	request := Request{
		Query:            "alpha",
		Terms:            []string{"alpha"},
		ExcludedTerms:    []string{"forbidden"},
		Phrases:          []string{"alpha phrase"},
		Source:           SourceLocal,
		Limit:            37,
		Fuzzy:            true,
		WithFacets:       true,
		MinDate:          time.Unix(10, 0),
		MaxDate:          time.Unix(20, 0),
		Offset:           9,
		ContentDomain:    ContentDomainImage,
		Language:         "en",
		SiteHost:         "example.com",
		InURL:            "path",
		TLD:              "com",
		FileType:         "html",
		Author:           "author",
		URLMaskFilter:    "mask",
		PreferMaskFilter: "prefer",
		Verify:           VerifyIfFresh,
		Navigation:       "hosts",
		SortByDate:       true,
		Near:             true,
		AllowWebFallback: true,
		SafeSearch:       true,
		Explain:          true,
	}
	if _, err := NewPseudoRelevanceSearcher(inner).Search(
		context.Background(),
		request,
	); err != nil {
		t.Fatalf("Search: %v", err)
	}
	expected := request
	expected.ExpansionTerms = []string{"central"}
	if inner.calls != 2 || !reflect.DeepEqual(inner.requests[1], expected) {
		t.Fatalf("expanded request = %#v", inner.requests[1])
	}
}

func assertNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("value = %.17g, want %.17g", got, want)
	}
}

func sumProbabilities(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}

	return total
}
