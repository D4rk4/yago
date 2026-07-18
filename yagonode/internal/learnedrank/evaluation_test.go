package learnedrank

import (
	"math"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestLinearRerankBoundsWindowPreservesMissingEvidenceAndExplainsSignals(t *testing.T) {
	ranker, err := NewRanker(3)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	model := mustLinearModel(t, linearWeights(map[int]float64{0: 1}))
	if err := ranker.Activate(mustSnapshot(t, "linear-v1", model)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	results := []searchcore.Result{
		rankingResult("https://low.example", 1, 10),
		{URL: "https://fixed.example", Score: 20},
		rankingResult("https://high.example", 3, 30),
		rankingResult("https://tail.example", 100, 40),
	}
	outcome, err := ranker.Rerank(
		searchcore.Request{Terms: []string{"alpha", "beta"}, Explain: true},
		results,
	)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	assertLinearRerankOutcome(t, outcome)
	if results[0].URL != "https://low.example" || results[0].Score != 10 {
		t.Fatalf("input results were mutated")
	}
}

func assertLinearRerankOutcome(t *testing.T, outcome Outcome) {
	t.Helper()
	if !outcome.Applied || outcome.SnapshotRevision != "linear-v1" ||
		outcome.ModelKind != ModelLinearLambdaRank {
		t.Fatalf("outcome metadata = %#v", outcome)
	}
	urls := resultURLs(outcome.Results)
	if !reflect.DeepEqual(urls, []string{
		"https://high.example",
		"https://fixed.example",
		"https://low.example",
		"https://tail.example",
	}) {
		t.Fatalf("reranked URLs = %v", urls)
	}
	if outcome.Results[1].Score != 20 || outcome.Results[3].Score != 40 {
		t.Fatalf("unchanged scores = %v, %v", outcome.Results[1].Score, outcome.Results[3].Score)
	}
	if len(outcome.Explanations) != 2 || outcome.Explanations[0].FinalRank != 1 ||
		outcome.Explanations[1].FinalRank != 3 {
		t.Fatalf("explanations = %#v", outcome.Explanations)
	}
	for _, explanation := range outcome.Explanations {
		if len(explanation.Signals) != len(rankingFeatures) || len(explanation.Trees) != 0 {
			t.Fatalf("linear explanation = %#v", explanation)
		}
		retrieval := explanation.Signals[0]
		if !retrieval.Known || !retrieval.Used || retrieval.Weight != 1 ||
			retrieval.Contribution != retrieval.NormalizedValue {
			t.Fatalf("retrieval explanation = %#v", retrieval)
		}
		if explanation.Signals[1].Known || explanation.Signals[1].Used ||
			explanation.Signals[1].NormalizedValue != 0 ||
			explanation.Signals[1].Contribution != 0 {
			t.Fatalf("unknown signal explanation = %#v", explanation.Signals[1])
		}
	}
}

func TestRerankFallsBackWithoutModelResultsOrEnoughEvidence(t *testing.T) {
	ranker, err := NewRanker(2)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	results := []searchcore.Result{{URL: "a", Score: 1}, {URL: "b", Score: 2}}
	outcome, err := ranker.Rerank(searchcore.Request{}, results)
	if err != nil || outcome.Applied || !reflect.DeepEqual(outcome.Results, results) {
		t.Fatalf("no-model outcome = %#v, %v", outcome, err)
	}
	if err := ranker.Activate(mustSnapshot(
		t,
		"linear",
		mustLinearModel(t, linearWeights(nil)),
	)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	empty, err := ranker.Rerank(searchcore.Request{}, nil)
	if err != nil || empty.Applied || empty.Results != nil {
		t.Fatalf("empty outcome = %#v, %v", empty, err)
	}
	nonNilEmpty := make([]searchcore.Result, 0)
	empty, err = ranker.Rerank(searchcore.Request{}, nonNilEmpty)
	if err != nil || empty.Applied || empty.Results == nil {
		t.Fatalf("non-nil empty outcome = %#v, %v", empty, err)
	}
	outcome, err = ranker.Rerank(searchcore.Request{}, results)
	if err != nil || outcome.Applied || !reflect.DeepEqual(outcome.Results, results) {
		t.Fatalf("no-evidence outcome = %#v, %v", outcome, err)
	}
	one := append([]searchcore.Result(nil), results...)
	one[0].Evidence = retrievalEvidence(1)
	outcome, err = ranker.Rerank(searchcore.Request{}, one)
	if err != nil || outcome.Applied || !reflect.DeepEqual(outcome.Results, one) {
		t.Fatalf("one-evidence outcome = %#v, %v", outcome, err)
	}
}

func TestHistogramRerankExposesTreePathsAndSupportsPredictionOnly(t *testing.T) {
	ranker, err := NewRanker(4)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	model := mustHistogramModel(t)
	if err := ranker.Activate(mustHistogramSnapshot(t, "tree-v1", model)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	results := []searchcore.Result{
		rankingResult("https://low.example", 0, 1),
		rankingResult("https://high.example", 3, 2),
		rankingResult("https://middle.example", 2, 3),
	}
	explained, err := ranker.Rerank(
		searchcore.Request{Query: "tree", Explain: true},
		results,
	)
	if err != nil {
		t.Fatalf("Rerank explained: %v", err)
	}
	if !explained.Applied || len(explained.Explanations) != 3 {
		t.Fatalf("tree outcome = %#v", explained)
	}
	for _, explanation := range explained.Explanations {
		if len(explanation.Trees) != 1 || explanation.Trees[0].TreeIndex != 1 ||
			len(explanation.Trees[0].Decisions) != 1 {
			t.Fatalf("tree explanation = %#v", explanation)
		}
		decision := explanation.Trees[0].Decisions[0]
		if decision.Signal != searchcore.SignalRetrievalScore ||
			!decision.Known || !explanation.Signals[0].Used ||
			explanation.Signals[0].NormalizedValue != decision.NormalizedValue {
			t.Fatalf("tree decision = %#v, signal = %#v", decision, explanation.Signals[0])
		}
	}
	predicted, err := ranker.Rerank(searchcore.Request{Query: "tree"}, results)
	if err != nil || !predicted.Applied || predicted.Explanations != nil {
		t.Fatalf("prediction-only outcome = %#v, %v", predicted, err)
	}
}

func TestStableInputTiesAndIdentityFallbacks(t *testing.T) {
	if got := []string{
		rankingIdentity(searchcore.Result{URLHash: "hash", URL: "url"}, 0),
		rankingIdentity(searchcore.Result{URL: "url"}, 1),
		rankingIdentity(searchcore.Result{DisplayURL: "display"}, 2),
		rankingIdentity(searchcore.Result{Title: "title"}, 3),
		rankingIdentity(searchcore.Result{}, 4),
	}; !reflect.DeepEqual(got, []string{
		"hash:hash",
		"url:url",
		"display_url:display",
		"title:title",
		"position:5",
	}) {
		t.Fatalf("identities = %v", got)
	}
	ranker, err := NewRanker(3)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	if err := ranker.Activate(mustSnapshot(
		t,
		"ties",
		mustLinearModel(t, linearWeights(nil)),
	)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	results := []searchcore.Result{
		rankingResult("https://b.example", 1, 1),
		rankingResult("https://a.example", 2, 2),
		rankingResult("https://a.example", 3, 3),
	}
	outcome, err := ranker.Rerank(searchcore.Request{}, results)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if got := resultURLs(outcome.Results); !reflect.DeepEqual(got, []string{
		"https://b.example",
		"https://a.example",
		"https://a.example",
	}) || outcome.Results[0].Score != 0 || outcome.Results[1].Score != 0 {
		t.Fatalf("tie order = %v, %#v", got, outcome.Results)
	}
}

func TestRerankCoversTheBoundedFusedCandidateWindow(t *testing.T) {
	ranker, err := NewRanker(3)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	if err := ranker.Activate(mustSnapshot(
		t,
		"fused-window",
		mustLinearModel(t, linearWeights(map[int]float64{0: 1})),
	)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	localLow := rankingResult("local-low", 1, 1)
	localLow.Source = searchcore.SourceGlobal
	remote := rankingResult("remote", 3, 2)
	remote.Source = searchcore.SourceRemote
	web := rankingResult("web", 5, 3)
	web.Source = searchcore.SourceWeb
	tail := rankingResult("tail", math.MaxFloat64, 4)
	tail.Source = searchcore.SourceRemote
	outcome, err := ranker.Rerank(
		searchcore.Request{Source: searchcore.SourceGlobal, Explain: true},
		[]searchcore.Result{localLow, remote, web, tail},
	)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if got := resultURLs(outcome.Results); !reflect.DeepEqual(
		got,
		[]string{"web", "remote", "local-low", "tail"},
	) || outcome.Results[3].Score != 4 || len(outcome.Explanations) != 3 {
		t.Fatalf("federated order = %v, %#v", got, outcome.Results)
	}
	if got := []string{
		outcome.Explanations[0].Identity,
		outcome.Explanations[1].Identity,
		outcome.Explanations[2].Identity,
	}; !reflect.DeepEqual(got, []string{"url:web", "url:remote", "url:local-low"}) {
		t.Fatalf("federated explanations = %v", got)
	}
}

func TestRerankRejectsInvalidEvidenceAndInternalModelCorruption(t *testing.T) {
	ranker, err := NewRanker(2)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	if err := ranker.Activate(mustSnapshot(
		t,
		"linear",
		mustLinearModel(t, linearWeights(nil)),
	)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	invalid := []searchcore.Result{
		rankingResult("a", math.MaxFloat64, 1),
		rankingResult("b", 1, 2),
	}
	outcome, err := ranker.Rerank(searchcore.Request{}, invalid)
	if err == nil || outcome.Applied || !reflect.DeepEqual(outcome.Results, invalid) {
		t.Fatalf("invalid evidence outcome = %#v, %v", outcome, err)
	}

	ranker.active.Load().kind = ModelKind("corrupt")
	valid := []searchcore.Result{
		rankingResult("a", 1, 1),
		rankingResult("b", 2, 2),
	}
	if _, err := ranker.Rerank(searchcore.Request{}, valid); err == nil {
		t.Fatalf("corrupt model kind was accepted")
	}

	ranker.active.Load().kind = ModelLinearLambdaRank
	ranker.candidateWindow = 2049
	many := make([]searchcore.Result, 2049)
	for index := range many {
		many[index] = rankingResult(string(rune(index+1)), float64(index), 0)
	}
	if _, err := ranker.Rerank(searchcore.Request{}, many); err == nil {
		t.Fatalf("oversized model query was accepted")
	}
}

func TestRankingConstructionAndEvaluationValidationFailures(t *testing.T) {
	vector, _, err := MapRankingEvidence(retrievalEvidence(1))
	if err != nil {
		t.Fatalf("MapRankingEvidence: %v", err)
	}
	candidate := rankingCandidate{
		originalIndex:      0,
		documentIdentifier: "document",
		identity:           "url:document",
		result:             rankingResult("document", 1, 1),
		features:           vector,
	}
	for _, request := range []searchcore.Request{
		{Query: "query"},
		{Terms: []string{"term"}},
		{},
	} {
		if _, err := rankingQueryGroup(request, []rankingCandidate{candidate}); err != nil {
			t.Fatalf("rankingQueryGroup: %v", err)
		}
	}
	if _, err := rankingQueryGroup(searchcore.Request{}, []rankingCandidate{{
		documentIdentifier: "invalid",
	}}); err == nil {
		t.Fatalf("invalid ranking example was accepted")
	}
	if _, err := rankingQueryGroup(searchcore.Request{}, nil); err == nil {
		t.Fatalf("empty ranking group was accepted")
	}

	valid := []candidateEvaluation{{documentIdentifier: "a"}, {documentIdentifier: "b"}}
	if _, err := validateEvaluations(valid, 2); err != nil {
		t.Fatalf("validateEvaluations: %v", err)
	}
	invalidEvaluations := []struct {
		values   []candidateEvaluation
		expected int
	}{
		{valid, 1},
		{[]candidateEvaluation{{}}, 1},
		{[]candidateEvaluation{{documentIdentifier: "a"}, {documentIdentifier: "a"}}, 2},
	}
	for index, invalid := range invalidEvaluations {
		if _, err := validateEvaluations(invalid.values, invalid.expected); err == nil {
			t.Fatalf("invalid evaluations %d accepted", index)
		}
	}
	predictions := []rankfit.RankedDocument{{DocumentIdentifier: "a", Rank: 1}}
	if evaluations, err := rankedDocumentEvaluations(predictions, 1); err != nil ||
		evaluations[0].rank != 1 {
		t.Fatalf("ranked document conversion = %#v, %v", evaluations, err)
	}
	got := candidateEvidence([]rankingCandidate{candidate})
	if got["document"].identity != "url:document" {
		t.Fatalf("candidate evidence = %#v", got)
	}
}

func TestModelEvaluationAndExplanationValidationFailures(t *testing.T) {
	vector, _, err := MapRankingEvidence(retrievalEvidence(1))
	if err != nil {
		t.Fatalf("MapRankingEvidence: %v", err)
	}
	example, err := rankfit.NewRankingExample("foreign", 0, vector)
	if err != nil {
		t.Fatalf("NewRankingExample: %v", err)
	}
	group, err := rankfit.NewQueryGroup("query", []rankfit.RankingExample{example})
	if err != nil {
		t.Fatalf("NewQueryGroup: %v", err)
	}
	candidates := []rankingCandidate{{
		documentIdentifier: "expected",
		result:             rankingResult("expected", 1, 1),
		features:           vector,
	}}
	linear := mustLinearModel(t, linearWeights(nil))
	histogram := mustHistogramModel(t)
	linearSnapshot := mustSnapshot(t, "linear", linear)
	histogramSnapshot := mustHistogramSnapshot(t, "tree", histogram)
	if _, err := linearSnapshot.evaluateLinear(group, candidates, true); err == nil {
		t.Fatalf("foreign linear explanation was accepted")
	}
	if _, err := histogramSnapshot.evaluateHistogram(group, candidates, true); err == nil {
		t.Fatalf("foreign tree explanation was accepted")
	}
	invalidLinear := Snapshot{kind: ModelLinearLambdaRank, linear: &rankfit.LinearLambdaRankModel{}}
	if _, err := invalidLinear.evaluateLinear(group, candidates, false); err == nil {
		t.Fatalf("invalid linear prediction was accepted")
	}
	if _, err := invalidLinear.evaluateLinear(group, candidates, true); err == nil {
		t.Fatalf("invalid linear explanation was accepted")
	}
	invalidHistogram := Snapshot{
		kind:      ModelHistogramLambdaMART,
		histogram: &rankfit.HistogramLambdaMARTModel{},
	}
	if _, err := invalidHistogram.evaluateHistogram(group, candidates, false); err == nil {
		t.Fatalf("invalid tree prediction was accepted")
	}
	if _, err := invalidHistogram.evaluateHistogram(group, candidates, true); err == nil {
		t.Fatalf("invalid tree explanation was accepted")
	}
	if _, err := (Snapshot{kind: ModelKind("future")}).evaluate(
		group,
		candidates,
		false,
	); err == nil {
		t.Fatalf("unknown model kind was evaluated")
	}

	raw := rawSignalExplanations(candidates[0].result.Evidence)
	if len(raw) != len(rankingFeatures) || !raw[0].Known || raw[1].Known {
		t.Fatalf("raw signal explanations = %#v", raw)
	}
}

func TestCandidateConstruction(t *testing.T) {
	candidates, err := rankingCandidates([]searchcore.Result{
		rankingResult("one", 1, 1),
		rankingResult("two", 2, 2),
		rankingResult("three", 3, 3),
	}, 2)
	if err != nil || len(candidates) != 2 ||
		candidates[0].features.Dimension() != len(rankingFeatures) {
		t.Fatalf("ranking candidates = %#v, %v", candidates, err)
	}
}

func TestTreeExplanationDistinguishesNeutralAndLegacyMissingEvidence(t *testing.T) {
	signals := rawSignalExplanations(retrievalEvidence(1))
	trees := treeExplanations([]rankfit.HistogramTreeContribution{
		{
			TreeIndex: 1,
			Decisions: []rankfit.HistogramTreeDecision{{
				FeatureName:       searchcore.SignalStrictScore.Name(),
				TerminatedMissing: true,
			}},
		},
		{
			TreeIndex: 2,
			Decisions: []rankfit.HistogramTreeDecision{{
				FeatureName: searchcore.SignalRelaxedScore.Name(),
				Threshold:   1,
				WentLeft:    true,
			}},
		},
	}, signals)
	if len(trees) != 2 || !trees[0].Decisions[0].TerminatedMissing ||
		signals[1].Used || !signals[3].Used ||
		trees[0].Decisions[0].Known || trees[1].Decisions[0].Known {
		t.Fatalf("missing tree explanations = %#v, %#v", trees, signals)
	}
}

func rankingResult(url string, retrieval, score float64) searchcore.Result {
	return searchcore.Result{
		URL:      url,
		Score:    score,
		Evidence: retrievalEvidence(retrieval),
	}
}

func retrievalEvidence(value float64) searchcore.RankingEvidence {
	return searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalRetrievalScore, Value: value},
	)
}

func resultURLs(results []searchcore.Result) []string {
	urls := make([]string, len(results))
	for index, result := range results {
		urls[index] = result.URL
	}

	return urls
}
