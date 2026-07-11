package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/yacysearch"
)

func TestClickCaptureAdapterRoundTrip(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	store, err := clickcapture.Open(v)
	if err != nil {
		t.Fatalf("clickcapture.Open: %v", err)
	}
	adapter := newClickCaptureAdapter(store, nil)
	candidates := []yacysearch.ImpressionCandidate{
		{URLIdentity: "https://a.example/", ClusterIdentity: "a", Position: 1},
		{URLIdentity: "https://b.example/", ClusterIdentity: "b", Position: 2},
	}
	prepared, err := adapter.PrepareImpression(t.Context(), "query", candidates)
	if err != nil {
		t.Fatalf("PrepareImpression: %v", err)
	}
	if prepared.Token == "" || len(prepared.Order) != 2 {
		t.Fatalf("prepared = %#v", prepared)
	}
	original := prepared.Order[0]
	if err := adapter.RecordClick(
		t.Context(),
		prepared.Token,
		candidates[original].URLIdentity,
		1,
	); err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	if err := adapter.RecordClick(
		t.Context(),
		prepared.Token,
		candidates[original].URLIdentity,
		1,
	); err == nil {
		t.Fatal("replayed click succeeded")
	}
}

func TestClickCaptureAdapterRunsActiveModelTeamDraft(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	store, err := clickcapture.Open(v)
	if err != nil {
		t.Fatal(err)
	}
	ranker, err := learnedrank.NewRanker(3)
	if err != nil {
		t.Fatal(err)
	}
	if err := ranker.Activate(rankingSnapshotFixture(t, "active-v2")); err != nil {
		t.Fatal(err)
	}
	adapter := newClickCaptureAdapter(store, ranker)
	candidates := []yacysearch.ImpressionCandidate{
		{
			URLIdentity: "https://good/", ClusterIdentity: "good", Position: 11,
			LexicalPosition: 13,
		},
		{
			URLIdentity: "https://middle/", ClusterIdentity: "middle", Position: 12,
			LexicalPosition: 12,
		},
		{
			URLIdentity: "https://bad/", ClusterIdentity: "bad", Position: 13,
			LexicalPosition: 11,
		},
	}
	prepared, err := adapter.PrepareImpression(t.Context(), "query", candidates)
	if err != nil || len(prepared.Order) != len(candidates) {
		t.Fatalf("PrepareImpression = %#v, %v", prepared, err)
	}
	aggregates, err := store.Aggregates(t.Context())
	if err != nil || len(aggregates) != 1 {
		t.Fatalf("Aggregates = %#v, %v", aggregates, err)
	}
	found := false
	for _, model := range aggregates[0].Models {
		if model.Interleaving != nil &&
			model.Interleaving.PrimaryRevision == "active-v2" &&
			model.Interleaving.SecondaryRevision == clickcapture.LexicalRevision {
			found = true
		}
	}
	if !found || activeRankingRevision(ranker) != "active-v2" {
		t.Fatalf("active comparison = %#v", aggregates)
	}
	portalAdapter := newPortalClickCaptureAdapter(store, ranker)
	if _, err := portalAdapter.PrepareImpression(
		t.Context(), "", []publicportal.ImpressionCandidate{{
			URLIdentity: "https://a/", ClusterIdentity: "a", Position: 1,
			LexicalPosition: 1,
		}},
	); err == nil {
		t.Fatal("invalid active portal impression succeeded")
	}
}

func TestLexicalCandidateOrderValidation(t *testing.T) {
	if activeRankingRevision(nil) != "" {
		t.Fatal("nil ranker has a revision")
	}
	inactive, err := learnedrank.NewRanker(2)
	if err != nil || activeRankingRevision(inactive) != "" {
		t.Fatalf("inactive ranker revision = %q, %v", activeRankingRevision(inactive), err)
	}
	for _, candidates := range [][]yacysearch.ImpressionCandidate{
		{{LexicalPosition: 0}},
		{{LexicalPosition: 1}, {LexicalPosition: 1}},
	} {
		if _, valid := lexicalCandidateOrder(candidates); valid {
			t.Fatalf("invalid lexical order accepted: %#v", candidates)
		}
	}
	order, valid := lexicalCandidateOrder([]yacysearch.ImpressionCandidate{
		{LexicalPosition: 2}, {LexicalPosition: 1},
	})
	if !valid || order[0] != 1 || order[1] != 0 {
		t.Fatalf("lexical order = %v, %v", order, valid)
	}
}

func TestPortalClickCaptureAdapterUsesSharedExperiment(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	store, err := clickcapture.Open(v)
	if err != nil {
		t.Fatal(err)
	}
	if newPortalClickCaptureAdapter(nil, nil) != nil {
		t.Fatal("nil store produced a portal adapter")
	}
	adapter := newPortalClickCaptureAdapter(store, nil)
	prepared, err := adapter.PrepareImpression(
		t.Context(),
		"query",
		[]publicportal.ImpressionCandidate{
			{
				URLIdentity: "https://a/", ClusterIdentity: "a", Position: 1,
				LexicalPosition: 2,
			},
			{
				URLIdentity: "https://b/", ClusterIdentity: "b", Position: 2,
				LexicalPosition: 1,
			},
		},
	)
	if err != nil || prepared.Token == "" || len(prepared.Order) != 2 {
		t.Fatalf("portal PrepareImpression = %#v, %v", prepared, err)
	}
}

func TestClickCaptureAdapterNilAndErrors(t *testing.T) {
	if newClickCaptureAdapter(nil, nil) != nil {
		t.Fatal("nil store produced an adapter")
	}
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	store, err := clickcapture.Open(v)
	if err != nil {
		t.Fatalf("clickcapture.Open: %v", err)
	}
	adapter := newClickCaptureAdapter(store, nil)
	if _, err := adapter.PrepareImpression(t.Context(), "", nil); err == nil {
		t.Fatal("invalid impression succeeded")
	}
	if err := adapter.RecordClick(t.Context(), "invalid", "identity", 1); err == nil {
		t.Fatal("invalid click succeeded")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := adapter.PrepareImpression(canceled, "query", []yacysearch.ImpressionCandidate{{
		URLIdentity: "url", ClusterIdentity: "cluster", Position: 1,
	}}); err == nil {
		t.Fatal("canceled impression succeeded")
	}
}
