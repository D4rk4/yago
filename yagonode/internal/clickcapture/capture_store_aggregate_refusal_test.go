package clickcapture

import (
	"fmt"
	"strings"
	"testing"
)

func TestStoreRefusesClicksBeyondRecordedImpressions(t *testing.T) {
	store := openClickStore(t)
	result := displayedFixture("https://a.example/")
	recorded := mustIssueToken(t, store.issuer, result)
	recordedClaims, err := store.issuer.parse(recorded)
	if err != nil {
		t.Fatalf("parse recorded: %v", err)
	}
	if err := store.recordImpression(t.Context(), recordedClaims); err != nil {
		t.Fatalf("recordImpression: %v", err)
	}
	if err := store.RecordClick(t.Context(), recorded, result[0].URLIdentity, 1); err != nil {
		t.Fatalf("click within recorded impressions: %v", err)
	}
	assertClusterEvidence(t, store, result[0].ClusterIdentity, 1, 1)

	unrecorded := mustIssueToken(t, store.issuer, result)
	err = store.RecordClick(t.Context(), unrecorded, result[0].URLIdentity, 1)
	if err == nil ||
		!strings.Contains(err.Error(), "signed impression result aggregate is unavailable") {
		t.Fatalf("click above recorded impressions = %v", err)
	}
	assertClusterEvidence(t, store, result[0].ClusterIdentity, 1, 1)

	second := mustIssueToken(t, store.issuer, result)
	secondClaims, err := store.issuer.parse(second)
	if err != nil {
		t.Fatalf("parse second: %v", err)
	}
	if err := store.recordImpression(t.Context(), secondClaims); err != nil {
		t.Fatalf("record second impression: %v", err)
	}
	if err := store.RecordClick(t.Context(), second, result[0].URLIdentity, 1); err != nil {
		t.Fatalf("click within the second recorded impression: %v", err)
	}
	assertClusterEvidence(t, store, result[0].ClusterIdentity, 2, 2)
}

func TestStoreRefusesImpressionModelAboveItsBound(t *testing.T) {
	store := openClickStore(t)
	for modelIndex := range maximumModelsPerQuery {
		if err := store.recordImpression(t.Context(), storeClaims(
			"bounded",
			fmt.Sprintf("model-%d", modelIndex),
			displayedFixture("url"),
		)); err != nil {
			t.Fatalf("record model %d: %v", modelIndex, err)
		}
	}
	err := store.recordImpression(t.Context(), storeClaims(
		"bounded",
		"one-too-many",
		displayedFixture("url"),
	))
	if err == nil || !strings.Contains(err.Error(), "impression model bound reached") {
		t.Fatalf("model above bound = %v", err)
	}
	aggregate := storedQueryEvidence(t, store, "bounded")
	if len(aggregate.Models) != maximumModelsPerQuery {
		t.Fatalf("stored models = %d", len(aggregate.Models))
	}
	if _, present := aggregate.Models["one-too-many"]; present {
		t.Fatalf("refused model was stored: %#v", aggregate.Models)
	}
}

func assertClusterEvidence(
	t *testing.T,
	store *Store,
	cluster string,
	impressions int,
	clicks int,
) {
	t.Helper()
	result := storedQueryEvidence(t, store, "query").Models["model"].Results[cluster]
	if result.Impressions != impressions || result.Clicks != clicks {
		t.Fatalf("cluster %q evidence = %#v", cluster, result)
	}
}

func storedQueryEvidence(t *testing.T, store *Store, query string) QueryEvidence {
	t.Helper()
	aggregates, err := store.Aggregates(t.Context())
	if err != nil {
		t.Fatalf("Aggregates: %v", err)
	}
	for _, aggregate := range aggregates {
		if aggregate.Query == query {
			return aggregate
		}
	}
	t.Fatalf("query %q is absent from %#v", query, aggregates)

	return QueryEvidence{}
}
