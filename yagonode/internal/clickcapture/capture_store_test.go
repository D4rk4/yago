package clickcapture

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestStorePreparesAuthenticatedImpressionsAndValidatedClicks(t *testing.T) {
	store := openClickStore(t)
	candidates := []Candidate{
		{URLIdentity: "https://a.example/", ClusterIdentity: "a", Position: 1},
		{URLIdentity: "https://b.example/", ClusterIdentity: "b", Position: 2},
		{URLIdentity: "https://c.example/", ClusterIdentity: "c", Position: 3},
	}
	prepared := prepareClickImpressions(t, store, candidates)
	clicked := measuredClickCandidate(prepared)
	if err := store.RecordClick(
		t.Context(), prepared.Token, clicked.URLIdentity, clicked.Position,
	); err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	assertStoredClickEvidence(t, store, clicked)
}

func prepareClickImpressions(
	t *testing.T,
	store *Store,
	candidates []Candidate,
) PreparedImpression {
	t.Helper()
	var prepared PreparedImpression
	for range 8 {
		var err error
		prepared, err = store.PrepareImpression(t.Context(), "  Go   Search ", candidates)
		if err != nil {
			t.Fatalf("PrepareImpression: %v", err)
		}
		if prepared.Token == "" || len(prepared.Candidates) != len(candidates) {
			t.Fatalf("prepared = %#v", prepared)
		}
	}

	return prepared
}

func measuredClickCandidate(prepared PreparedImpression) DisplayedCandidate {
	clicked := prepared.Candidates[0]
	for _, candidate := range prepared.Candidates {
		if measuredPropensity(candidate.Propensity) {
			clicked = candidate

			break
		}
	}

	return clicked
}

func assertStoredClickEvidence(t *testing.T, store *Store, clicked DisplayedCandidate) {
	t.Helper()
	aggregates, err := store.Aggregates(t.Context())
	if err != nil {
		t.Fatalf("Aggregates: %v", err)
	}
	if len(aggregates) != 1 || aggregates[0].Query != "go search" ||
		aggregates[0].ObservedAtUnix != 1_800_000_000 {
		t.Fatalf("aggregates = %#v", aggregates)
	}
	model := aggregates[0].Models[adjacentPairModelAssignment]
	if model.Impressions != 8 || model.RandomizedImpressions == 0 {
		t.Fatalf("model evidence = %#v", model)
	}
	result := model.Results[clicked.ClusterIdentity]
	if result.Clicks != 1 || result.Impressions != 8 {
		t.Fatalf("clicked result evidence = %#v", result)
	}
	if measuredPropensity(clicked.Propensity) && result.ClippedClickWeight == 0 {
		t.Fatalf("randomized click lacks weight: %#v", result)
	}
	judgments, err := store.ImplicitJudgments(t.Context(), 1)
	if err != nil {
		t.Fatalf("ImplicitJudgments: %v", err)
	}
	if len(judgments) != 1 ||
		!judgments[0].ObservedAt.Equal(time.Unix(1_800_000_000, 0).UTC()) {
		t.Fatalf("implicit judgment time = %#v", judgments)
	}
}

func TestStoreRejectsUnrecordedAndReplayedClicks(t *testing.T) {
	store := openClickStore(t)
	result := displayedFixture("https://a.example/")
	token := mustIssueToken(t, store.issuer, result)
	if err := store.RecordClick(t.Context(), token, result[0].URLIdentity, 1); err == nil {
		t.Fatal("click without recorded impression succeeded")
	}
	claims, err := store.issuer.parse(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := store.recordImpression(t.Context(), claims); err != nil {
		t.Fatalf("recordImpression: %v", err)
	}
	secondToken := mustIssueToken(t, store.issuer, result)
	secondClaims, err := store.issuer.parse(secondToken)
	if err != nil {
		t.Fatalf("parse second: %v", err)
	}
	if err := store.recordImpression(t.Context(), secondClaims); err != nil {
		t.Fatalf("record second impression: %v", err)
	}
	if err := store.RecordClick(t.Context(), secondToken, result[0].URLIdentity, 1); err != nil {
		t.Fatalf("first click: %v", err)
	}
	if err := store.RecordClick(t.Context(), secondToken, result[0].URLIdentity, 1); err == nil {
		t.Fatal("replayed click succeeded")
	}
}

func TestStoreBoundsModelsResultsQueriesAndCounters(t *testing.T) {
	store := openClickStore(t)
	for modelIndex := range maximumModelsPerQuery {
		claims := storeClaims("q", fmt.Sprintf("model-%d", modelIndex), displayedFixture("url"))
		if err := store.recordImpression(t.Context(), claims); err != nil {
			t.Fatalf("record model %d: %v", modelIndex, err)
		}
	}
	if err := store.recordImpression(
		t.Context(),
		storeClaims("q", "one-too-many", displayedFixture("url")),
	); err == nil {
		t.Fatal("model above bound succeeded")
	}

	for batch := range 8 {
		results := make([]DisplayedCandidate, 10)
		for index := range results {
			identity := fmt.Sprintf("cluster-%02d", batch*10+index)
			results[index] = candidateFixture(
				Candidate{
					URLIdentity:     "https://example/" + identity,
					ClusterIdentity: identity,
					Position:        index + 1,
				},
				0.5,
				AttributionOriginal,
				index,
			)
		}
		if err := store.recordImpression(
			t.Context(),
			storeClaims("bounded", "model", results),
		); err != nil {
			t.Fatalf("record batch %d: %v", batch, err)
		}
	}
	aggregates, err := store.Aggregates(t.Context())
	if err != nil {
		t.Fatalf("Aggregates: %v", err)
	}
	var bounded QueryEvidence
	for _, aggregate := range aggregates {
		if aggregate.Query == "bounded" {
			bounded = aggregate
		}
	}
	if len(bounded.Models["model"].Results) != maximumResultsPerModel {
		t.Fatalf("bounded results = %d", len(bounded.Models["model"].Results))
	}

	full := newQueryEvidence("full")
	full.Models["model"] = ModelEvidence{
		Assignment:            "model",
		Impressions:           maximumAggregateValue,
		RandomizedImpressions: maximumAggregateValue,
		Results: map[string]ResultEvidence{
			"cluster": {
				URLIdentity:           "url",
				ClusterIdentity:       "cluster",
				Impressions:           maximumAggregateValue,
				RandomizedImpressions: maximumAggregateValue,
				Clicks:                maximumAggregateValue,
				ClippedExposureWeight: maximumAggregateWeight,
				ClippedClickWeight:    maximumAggregateWeight,
			},
		},
	}
	if incrementAggregate(maximumAggregateValue) != maximumAggregateValue ||
		addAggregateWeight(maximumAggregateWeight, 1) != maximumAggregateWeight {
		t.Fatal("aggregate saturation failed")
	}
	if queryImpressions(full) != maximumAggregateValue {
		t.Fatal("query impression saturation failed")
	}
}

func TestStoreQueryCapacityEvictsWeakest(t *testing.T) {
	store := openClickStore(t)
	if err := store.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range maximumStoredQueries {
			evidence := newQueryEvidence(fmt.Sprintf("q-%04d", index))
			if index == 0 {
				evidence.Models["model"] = ModelEvidence{
					Assignment:  "model",
					Impressions: 2,
					Results:     map[string]ResultEvidence{},
				}
			}
			if err := store.records.Put(tx, vault.Key(evidence.Query), evidence); err != nil {
				return fmt.Errorf("seed query capacity: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("seed capacity: %v", err)
	}
	if err := store.recordImpression(
		t.Context(),
		storeClaims("new-query", "model", displayedFixture("url")),
	); err != nil {
		t.Fatalf("record at capacity: %v", err)
	}
	aggregates, err := store.Aggregates(t.Context())
	if err != nil {
		t.Fatalf("Aggregates: %v", err)
	}
	if len(aggregates) != maximumStoredQueries {
		t.Fatalf("query count = %d", len(aggregates))
	}
	present := map[string]bool{}
	for _, aggregate := range aggregates {
		present[aggregate.Query] = true
	}
	if !present["q-0000"] || !present["new-query"] || present["q-4095"] {
		t.Fatalf("capacity eviction set is incorrect")
	}
}

func TestEvidenceCodecMigrationRoundTripAndRejection(t *testing.T) {
	legacy, err := evidenceCodec{}.Decode([]byte(
		`{"query":" Legacy   Query ","urls":{"https://a/":{"clicks":99,"weight":100}}}`,
	))
	if err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	if legacy.Query != "legacy query" || len(legacy.Models) != 0 {
		t.Fatalf("legacy evidence = %#v", legacy)
	}
	current := newQueryEvidence("query")
	encoded, err := evidenceCodec{}.Encode(current)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := evidenceCodec{}.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Query != current.Query || decoded.Version != clickEvidenceVersion {
		t.Fatalf("decoded = %#v", decoded)
	}
	invalid := [][]byte{
		[]byte("{"),
		[]byte(`{}`),
		[]byte(`{"query":{}}`),
		[]byte(`{"version":3,"query":"q","models":{}}`),
		[]byte(`{"version":2,"query":"q","models":[]}`),
		[]byte(`{"version":2,"query":"","models":{}}`),
	}
	for _, raw := range invalid {
		if _, err := (evidenceCodec{}).Decode(raw); err == nil {
			t.Fatalf("invalid evidence %q decoded", raw)
		}
	}
	invalidFloat := newQueryEvidence("query")
	invalidFloat.Models["model"] = ModelEvidence{
		Assignment: "model",
		Results: map[string]ResultEvidence{
			"cluster": {
				URLIdentity:           "url",
				ClusterIdentity:       "cluster",
				ClippedExposureWeight: math.NaN(),
			},
		},
	}
	if _, err := (evidenceCodec{}).Encode(invalidFloat); err == nil {
		t.Fatal("invalid float encoded")
	}
}

func TestEvidenceCodecPersistsOnlyAggregates(t *testing.T) {
	evidence := queryEvidenceFixture("query", 3, map[string]ResultEvidence{
		"cluster": evidenceFixture("cluster", 3, 3, 1, evidenceWeights{6, 2}),
	})
	encoded, err := evidenceCodec{}.Encode(evidence)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	serialized := string(encoded)
	for _, forbidden := range []string{"token", "nonce", "session", "user", "event"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("persisted evidence contains %q: %s", forbidden, serialized)
		}
	}
	for _, required := range []string{"impressions", "clicks", "model", "cluster"} {
		if !strings.Contains(serialized, required) {
			t.Fatalf("persisted evidence lacks %q: %s", required, serialized)
		}
	}
}

func TestValidateQueryEvidenceRejectsInvalidAggregates(t *testing.T) {
	validResult := evidenceWithURL(
		evidenceFixture("cluster", 2, 1, 1, evidenceWeights{2, 1}),
		"url",
	)
	valid := newQueryEvidence("query")
	valid.Models["model"] = ModelEvidence{
		Assignment:            "model",
		Impressions:           2,
		RandomizedImpressions: 1,
		Results:               map[string]ResultEvidence{"cluster": validResult},
	}
	if err := validateQueryEvidence(valid); err != nil {
		t.Fatalf("valid evidence: %v", err)
	}
	invalid := []QueryEvidence{
		{Version: clickEvidenceVersion, Query: "query", ObservedAtUnix: -1},
		{Version: clickEvidenceVersion, Query: "query"},
		withEvidenceModels(valid, oversizedModels()),
		withEvidenceModel(valid, " bad", valid.Models["model"]),
		withEvidenceModel(valid, "model", ModelEvidence{
			Assignment: "model", Impressions: -1, Results: map[string]ResultEvidence{},
		}),
		withEvidenceModel(valid, "model", ModelEvidence{
			Assignment: "model", Impressions: 1, RandomizedImpressions: 2,
			Results: map[string]ResultEvidence{},
		}),
		withEvidenceModel(valid, "model", ModelEvidence{
			Assignment: "model", Results: nil,
		}),
		withEvidenceModel(valid, "model", ModelEvidence{
			Assignment: "model", Results: oversizedResults(),
		}),
		withEvidenceResult(valid, "other", validResult),
		withEvidenceResult(valid, "cluster", evidenceWithURL(
			evidenceFixture("cluster", 1, 1, 0, evidenceWeights{1, 0}), "",
		)),
		withEvidenceResult(
			valid,
			"cluster",
			evidenceFixture("cluster", -1, 0, 0, evidenceWeights{}),
		),
		withEvidenceResult(
			valid,
			"cluster",
			evidenceFixture("cluster", 1, 2, 0, evidenceWeights{}),
		),
		withEvidenceResult(
			valid,
			"cluster",
			evidenceFixture("cluster", 1, 1, 2, evidenceWeights{2, 1}),
		),
		withEvidenceResult(
			valid,
			"cluster",
			evidenceFixture("cluster", 1, 1, 1, evidenceWeights{-1, 0}),
		),
		withEvidenceResult(
			valid,
			"cluster",
			evidenceFixture("cluster", 1, 1, 1, evidenceWeights{1, 2}),
		),
	}
	for index, evidence := range invalid {
		if err := validateQueryEvidence(evidence); err == nil {
			t.Fatalf("invalid evidence %d validated", index)
		}
	}
}

func TestStoreMissingModelAndResultAggregates(t *testing.T) {
	store := openClickStore(t)
	result := displayedFixture("https://a.example/")
	missingModelToken := mustIssueToken(t, store.issuer, result)
	missingModelClaims, err := store.issuer.parse(missingModelToken)
	if err != nil {
		t.Fatalf("parse missing model: %v", err)
	}
	if err := store.recordImpression(t.Context(), missingModelClaims); err != nil {
		t.Fatalf("record missing model fixture: %v", err)
	}
	if err := store.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, _, getErr := store.records.Get(tx, vault.Key(missingModelClaims.query))
		if getErr != nil {
			return fmt.Errorf("get missing model fixture: %w", getErr)
		}
		delete(record.Models, missingModelClaims.modelAssignment)

		return store.records.Put(tx, vault.Key(record.Query), record)
	}); err != nil {
		t.Fatalf("remove model: %v", err)
	}
	if err := store.RecordClick(
		t.Context(),
		missingModelToken,
		result[0].URLIdentity,
		1,
	); err == nil {
		t.Fatal("click with missing model aggregate succeeded")
	}

	missingResultToken := mustIssueToken(t, store.issuer, result)
	missingResultClaims, err := store.issuer.parse(missingResultToken)
	if err != nil {
		t.Fatalf("parse missing result: %v", err)
	}
	if err := store.recordImpression(t.Context(), missingResultClaims); err != nil {
		t.Fatalf("record missing result fixture: %v", err)
	}
	if err := store.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, _, getErr := store.records.Get(tx, vault.Key(missingResultClaims.query))
		if getErr != nil {
			return fmt.Errorf("get missing result fixture: %w", getErr)
		}
		model := record.Models[missingResultClaims.modelAssignment]
		delete(model.Results, result[0].ClusterIdentity)
		record.Models[missingResultClaims.modelAssignment] = model

		return store.records.Put(tx, vault.Key(record.Query), record)
	}); err != nil {
		t.Fatalf("remove result: %v", err)
	}
	if err := store.RecordClick(
		t.Context(),
		missingResultToken,
		result[0].URLIdentity,
		1,
	); err == nil {
		t.Fatal("click with missing result aggregate succeeded")
	}
}

func TestStoreRejectsMismatchedAggregateKey(t *testing.T) {
	store := openClickStore(t)
	if err := store.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return store.records.Put(tx, vault.Key("query"), newQueryEvidence("other"))
	}); err != nil {
		t.Fatalf("seed mismatch: %v", err)
	}
	if err := store.recordImpression(
		t.Context(),
		storeClaims("query", "model", displayedFixture("url")),
	); err == nil {
		t.Fatal("mismatched aggregate key succeeded")
	}
}

func TestStoreOpenAndVaultErrors(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if _, err := OpenWithSources(v, &failingEntropy{}, time.Now); err == nil {
		t.Fatal("OpenWithSources accepted missing entropy")
	}
	seedVault, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("seed memvault.Open: %v", err)
	}
	seedStore, err := OpenWithSources(
		seedVault,
		&failingEntropy{remaining: impressionKeyBytes},
		time.Now,
	)
	if err != nil {
		t.Fatalf("seed OpenWithSources: %v", err)
	}
	if _, err := seedStore.PrepareImpression(t.Context(), "q", []Candidate{{
		URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
	}}); err == nil {
		t.Fatal("PrepareImpression accepted missing seed entropy")
	}
	nonceVault, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("nonce memvault.Open: %v", err)
	}
	nonceStore, err := OpenWithSources(
		nonceVault,
		&failingEntropy{remaining: impressionKeyBytes + 8},
		time.Now,
	)
	if err != nil {
		t.Fatalf("nonce OpenWithSources: %v", err)
	}
	if _, err := nonceStore.PrepareImpression(t.Context(), "q", []Candidate{{
		URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
	}}); err == nil {
		t.Fatal("PrepareImpression accepted missing nonce entropy")
	}
	store, err := OpenWithSources(v, &sequenceEntropy{}, time.Now)
	if err != nil {
		t.Fatalf("OpenWithSources: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatal("duplicate Open succeeded")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.PrepareImpression(canceled, "q", []Candidate{{
		URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
	}}); err == nil {
		t.Fatal("canceled PrepareImpression succeeded")
	}
	if _, err := store.Aggregates(canceled); err == nil {
		t.Fatal("canceled Aggregates succeeded")
	}
	if _, err := store.ImplicitJudgments(canceled, 1); err == nil {
		t.Fatal("canceled ImplicitJudgments succeeded")
	}
}

func TestStoreHelpers(t *testing.T) {
	if !measuredPropensity(0.5) || measuredPropensity(0) ||
		measuredPropensity(math.NaN()) || measuredPropensity(math.Inf(1)) ||
		measuredPropensity(2) {
		t.Fatal("measured propensity classification failed")
	}
	if inversePropensity(minimumMeasuredPropensity) != maximumInversePropensity ||
		inversePropensity(0.5) != 2 {
		t.Fatal("inverse propensity clipping failed")
	}
	if representativeURL("b", "a") != "a" || representativeURL("a", "b") != "a" ||
		representativeURL("", "b") != "b" {
		t.Fatal("representative URL selection failed")
	}
	results := map[string]ResultEvidence{
		"best":  {RandomizedImpressions: 2, Clicks: 1, Impressions: 2},
		"weak":  {RandomizedImpressions: 1, Clicks: 1, Impressions: 2},
		"tie-z": {RandomizedImpressions: 1, Clicks: 1, Impressions: 2},
	}
	evictWeakestResult(results)
	if _, present := results["weak"]; present {
		t.Fatal("deterministic weakest result was not evicted")
	}
	if !weakerResult(
		ResultEvidence{RandomizedImpressions: 1},
		ResultEvidence{RandomizedImpressions: 2},
	) || !weakerResult(
		ResultEvidence{RandomizedImpressions: 1, Clicks: 1},
		ResultEvidence{RandomizedImpressions: 1, Clicks: 2},
	) || !weakerResult(
		ResultEvidence{RandomizedImpressions: 1, Clicks: 1, Impressions: 1},
		ResultEvidence{RandomizedImpressions: 1, Clicks: 1, Impressions: 2},
	) {
		t.Fatal("weak result ordering failed")
	}
}

func openClickStore(t *testing.T) *Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	clock := newMutableClock(time.Unix(1_800_000_000, 0))
	store, err := OpenWithSources(v, &sequenceEntropy{}, clock.Now)
	if err != nil {
		t.Fatalf("OpenWithSources: %v", err)
	}

	return store
}

func storeClaims(query, model string, results []DisplayedCandidate) impressionClaims {
	return impressionClaims{
		query:           normalizeQuery(query),
		modelAssignment: model,
		results:         results,
	}
}

func withEvidenceModels(
	evidence QueryEvidence,
	models map[string]ModelEvidence,
) QueryEvidence {
	evidence.Models = models

	return evidence
}

func withEvidenceModel(
	evidence QueryEvidence,
	assignment string,
	model ModelEvidence,
) QueryEvidence {
	evidence.Models = map[string]ModelEvidence{assignment: model}

	return evidence
}

func withEvidenceResult(
	evidence QueryEvidence,
	identity string,
	result ResultEvidence,
) QueryEvidence {
	model := evidence.Models["model"]
	model.Results = map[string]ResultEvidence{identity: result}
	evidence.Models = map[string]ModelEvidence{"model": model}

	return evidence
}

func oversizedModels() map[string]ModelEvidence {
	models := make(map[string]ModelEvidence, maximumModelsPerQuery+1)
	for index := range maximumModelsPerQuery + 1 {
		assignment := fmt.Sprintf("model-%d", index)
		models[assignment] = ModelEvidence{
			Assignment: assignment,
			Results:    map[string]ResultEvidence{},
		}
	}

	return models
}

func oversizedResults() map[string]ResultEvidence {
	results := make(map[string]ResultEvidence, maximumResultsPerModel+1)
	for index := range maximumResultsPerModel + 1 {
		identity := fmt.Sprintf("cluster-%d", index)
		results[identity] = ResultEvidence{URLIdentity: "url", ClusterIdentity: identity}
	}

	return results
}
