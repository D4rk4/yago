package clickcapture

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestStorePersistsTeamDraftOutcomeWithoutRelevanceLabels(t *testing.T) {
	store := openClickStore(t)
	primary := []Candidate{
		{URLIdentity: "https://a/", ClusterIdentity: "a", Position: 11},
		{URLIdentity: "https://b/", ClusterIdentity: "b", Position: 12},
		{URLIdentity: "https://c/", ClusterIdentity: "c", Position: 13},
	}
	secondary := []Candidate{primary[2], primary[1], primary[0]}
	prepared, err := store.PrepareTeamDraft(
		t.Context(), "query", draftRanking("active-v2", primary),
		draftRanking(LexicalRevision, secondary), len(primary),
	)
	if err != nil || len(prepared.Candidates) != 3 {
		t.Fatalf("PrepareTeamDraft = %#v, %v", prepared, err)
	}
	clicked := map[string]bool{}
	for index, candidate := range prepared.Candidates {
		if candidate.Position != 11+index {
			t.Fatalf("candidate positions = %#v", prepared.Candidates)
		}
		if clicked[candidate.Attribution] {
			continue
		}
		if err := store.RecordClick(
			t.Context(), prepared.Token, candidate.URLIdentity, candidate.Position,
		); err != nil {
			t.Fatalf("RecordClick %s: %v", candidate.Attribution, err)
		}
		clicked[candidate.Attribution] = true
	}
	if !clicked[AttributionPrimary] || !clicked[AttributionSecondary] {
		t.Fatalf("draft attribution = %#v", prepared.Candidates)
	}
	if _, err := store.PrepareTeamDraft(
		t.Context(), "query", draftRanking("active-v2", primary),
		draftRanking(LexicalRevision, secondary), len(primary),
	); err != nil {
		t.Fatalf("second PrepareTeamDraft: %v", err)
	}
	aggregates, err := store.Aggregates(t.Context())
	if err != nil || len(aggregates) != 1 {
		t.Fatalf("Aggregates = %#v, %v", aggregates, err)
	}
	for assignment, model := range aggregates[0].Models {
		if !strings.HasPrefix(assignment, teamDraftAssignmentPrefix) ||
			model.Interleaving == nil || model.Interleaving.Impressions != 2 ||
			model.Interleaving.PrimaryRevision != "active-v2" ||
			model.Interleaving.SecondaryRevision != LexicalRevision ||
			model.Interleaving.PrimaryClicks != 1 ||
			model.Interleaving.SecondaryClicks != 1 {
			t.Fatalf("interleaving model = %q %#v", assignment, model)
		}
	}
	judgments, err := store.ImplicitJudgments(t.Context(), 1)
	if err != nil || len(judgments) != 0 {
		t.Fatalf("interleaving judgments = %#v, %v", judgments, err)
	}
}

func TestTeamDraftPreparationValidationAndHelpers(t *testing.T) {
	store := openClickStore(t)
	candidate := []Candidate{{URLIdentity: "url", ClusterIdentity: "cluster", Position: 1}}
	for _, revisions := range [][2]string{{"", "lexical"}, {"same", "same"}, {"bad/rev", "lexical"}} {
		if _, err := store.PrepareTeamDraft(
			t.Context(), "query", draftRanking(revisions[0], candidate),
			draftRanking(revisions[1], candidate), 1,
		); err == nil {
			t.Fatalf("invalid revisions accepted: %q", revisions)
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.PrepareTeamDraft(
		canceled, "query", draftRanking("model", candidate),
		draftRanking(LexicalRevision, candidate), 1,
	); err == nil {
		t.Fatal("canceled team draft succeeded")
	}
	if firstCandidatePosition(nil) != 1 ||
		firstCandidatePosition([]Candidate{{Position: 8}}, []Candidate{{Position: 4}}) != 4 {
		t.Fatal("first candidate position is incorrect")
	}
	current := &InterleavingOutcome{
		PrimaryRevision: "one", SecondaryRevision: "two", Impressions: 2,
	}
	if mergeInterleavingOutcome(current, InterleavingOutcome{
		PrimaryRevision: "different", SecondaryRevision: "two", Impressions: 1,
	}) != current {
		t.Fatal("mismatched interleaving outcome changed")
	}
}

func TestTeamDraftPreparationReportsSeedAndTokenFailures(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	seedStore, err := OpenWithSources(
		v,
		&failingEntropy{remaining: impressionKeyBytes},
		time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	candidate := []Candidate{{URLIdentity: "url", ClusterIdentity: "cluster", Position: 1}}
	if _, err := seedStore.PrepareTeamDraft(
		t.Context(), "query", draftRanking("model", candidate),
		draftRanking(LexicalRevision, candidate), 1,
	); err == nil {
		t.Fatal("missing team-draft seed succeeded")
	}
	nonceVault, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	nonceStore, err := OpenWithSources(
		nonceVault,
		&failingEntropy{remaining: impressionKeyBytes + 8},
		time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nonceStore.PrepareTeamDraft(
		t.Context(), "", draftRanking("model", candidate),
		draftRanking(LexicalRevision, candidate), 1,
	); err == nil {
		t.Fatal("invalid team-draft token succeeded")
	}
}

func draftRanking(revision string, candidates []Candidate) DraftRanking {
	return DraftRanking{Revision: revision, Candidates: candidates}
}

func TestInterleavingOutcomeValidationAndBounds(t *testing.T) {
	assignment, err := teamDraftAssignment("model", LexicalRevision)
	if err != nil {
		t.Fatal(err)
	}
	valid := &InterleavingOutcome{
		PrimaryRevision: "model", SecondaryRevision: LexicalRevision, Impressions: 1,
	}
	if validateInterleavingEvidence("anything", nil) != nil ||
		validateInterleavingEvidence(assignment, valid) != nil {
		t.Fatal("valid interleaving evidence rejected")
	}
	invalid := []struct {
		assignment string
		outcome    *InterleavingOutcome
	}{
		{"wrong", valid},
		{assignment, &InterleavingOutcome{
			PrimaryRevision: "same", SecondaryRevision: "same", Impressions: 1,
		}},
		{assignment, &InterleavingOutcome{
			PrimaryRevision: "model", SecondaryRevision: LexicalRevision,
		}},
		{assignment, &InterleavingOutcome{
			PrimaryRevision: "model", SecondaryRevision: LexicalRevision,
			Impressions: 1, PrimaryClicks: maximumClicksPerImpression + 1,
		}},
	}
	for index, candidate := range invalid {
		if validateInterleavingEvidence(candidate.assignment, candidate.outcome) == nil {
			t.Fatalf("invalid interleaving %d accepted", index)
		}
	}
	for _, revision := range []string{
		"", " bad", "bad/revision", strings.Repeat("a", 129),
	} {
		if validRankingRevision(revision) {
			t.Fatalf("invalid revision %q accepted", revision)
		}
	}
	if !validRankingRevision("Model_v2.1") {
		t.Fatal("valid revision rejected")
	}
	merged := mergeInterleavingOutcome(nil, *valid)
	merged = mergeInterleavingOutcome(merged, *valid)
	if merged.Impressions != 2 {
		t.Fatalf("merged outcome = %#v", merged)
	}
	merged.Impressions = maximumAggregateValue
	if mergeInterleavingOutcome(merged, *valid).Impressions != maximumAggregateValue {
		t.Fatal("interleaving impressions did not saturate")
	}
	model := ModelEvidence{}
	addInterleavingClick(&model, AttributionPrimary)
	model.Interleaving = &InterleavingOutcome{
		Impressions: 1, PrimaryClicks: maximumClicksPerImpression,
	}
	addInterleavingClick(&model, AttributionSecondary)
	if model.Interleaving.SecondaryClicks != 0 {
		t.Fatal("bounded interleaving click changed")
	}
	model.Interleaving.PrimaryClicks = 0
	addInterleavingClick(&model, "unknown")
	if model.Interleaving.PrimaryClicks != 0 {
		t.Fatal("unknown interleaving attribution changed")
	}
}
