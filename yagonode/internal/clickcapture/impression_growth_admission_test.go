package clickcapture

import (
	"errors"
	"testing"
)

type impressionGrowthAdmission struct {
	err   error
	calls int
}

func (admission *impressionGrowthAdmission) CheckGrowth() error {
	admission.calls++

	return admission.err
}

func TestNewImpressionsRespectStoragePressureAndClicksSettle(t *testing.T) {
	store := openClickStore(t)
	candidates := []Candidate{
		{URLIdentity: "https://a.example/", ClusterIdentity: "a", Position: 1},
		{URLIdentity: "https://b.example/", ClusterIdentity: "b", Position: 2},
	}
	prepared := prepareClickImpressions(t, store, candidates)
	admission := &impressionGrowthAdmission{err: errors.New("pressure")}
	store.AdmitImpressionGrowth(admission)
	if _, err := store.PrepareImpression(t.Context(), "query", candidates); err == nil {
		t.Fatal("fair-pair impression admitted under storage pressure")
	}
	if _, err := store.PrepareTeamDraft(
		t.Context(),
		"query",
		DraftRanking{Revision: "primary", Candidates: candidates},
		DraftRanking{Revision: "secondary", Candidates: candidates},
		len(candidates),
	); err == nil {
		t.Fatal("team-draft impression admitted under storage pressure")
	}
	clicked := measuredClickCandidate(prepared)
	if err := store.RecordClick(
		t.Context(),
		prepared.Token,
		clicked.URLIdentity,
		clicked.Position,
	); err != nil {
		t.Fatalf("settle admitted click under pressure: %v", err)
	}
	if admission.calls != 2 {
		t.Fatalf("impression growth checks = %d, want 2", admission.calls)
	}
}

func TestNilImpressionGrowthAdmissionAllowsPreparation(t *testing.T) {
	store := openClickStore(t)
	store.AdmitImpressionGrowth(nil)
	if _, err := store.PrepareImpression(t.Context(), "query", []Candidate{{
		URLIdentity: "https://a.example/", ClusterIdentity: "a", Position: 1,
	}}); err != nil {
		t.Fatalf("prepare without growth admission: %v", err)
	}
}

func TestHealthyImpressionGrowthAdmissionAllowsPreparation(t *testing.T) {
	store := openClickStore(t)
	admission := &impressionGrowthAdmission{}
	store.AdmitImpressionGrowth(admission)
	if _, err := store.PrepareImpression(t.Context(), "query", []Candidate{{
		URLIdentity: "https://a.example/", ClusterIdentity: "a", Position: 1,
	}}); err != nil {
		t.Fatalf("prepare with healthy growth admission: %v", err)
	}
	if admission.calls != 1 {
		t.Fatalf("healthy impression growth checks = %d, want 1", admission.calls)
	}
}
