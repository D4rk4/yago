package adminui

import (
	"strings"
	"testing"
)

func TestYagoRankBoundsRetrievalDiagnostics(t *testing.T) {
	t.Parallel()

	source := &searchExplanationFixture{explanation: SearchExplanation{
		Query:   "bounded",
		Results: []SearchExplanationResult{completeSearchExplanationResult()},
	}}
	console := New(Options{
		Ranking:           &fakeRanking{profile: sampleRankingProfile()},
		SearchExplanation: source,
	})
	body := do(t, console, "/admin/yagorank?q=bounded").body
	if !strings.Contains(body, `class="cds-code cds-code--bounded-explain"`) {
		t.Fatalf("bounded retrieval diagnostic class missing: %s", body)
	}
	stylesheet := do(t, console, "/admin/assets/carbon.css").body
	for _, want := range []string{
		".cds-code--bounded-explain",
		"max-height: 20rem",
		"overflow: auto",
		"white-space: pre-wrap",
		"overflow-wrap: anywhere",
	} {
		if !strings.Contains(stylesheet, want) {
			t.Fatalf("bounded retrieval diagnostic style missing %q", want)
		}
	}
}

func TestYagoRankSearchExplanationUsesReachableTableWidth(t *testing.T) {
	t.Parallel()

	source := &searchExplanationFixture{explanation: SearchExplanation{
		Query:   "reachable",
		Results: []SearchExplanationResult{completeSearchExplanationResult()},
	}}
	console := New(Options{
		Ranking:           &fakeRanking{profile: sampleRankingProfile()},
		SearchExplanation: source,
	})
	body := do(t, console, "/admin/yagorank?q=reachable").body
	for _, want := range []string{
		`class="cds-search-explain" aria-labelledby="search-explain-heading"`,
		`class="cds-scroll-x" role="region" aria-label="Search explanation results" tabindex="0"`,
		`<table class="cds-table">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("search explanation layout missing %q", want)
		}
	}
	if strings.Contains(
		body,
		`class="cds-settings-form" aria-labelledby="search-explain-heading"`,
	) {
		t.Fatal("search explanation retained the narrow settings-form container")
	}

	stylesheet := do(t, console, "/admin/assets/photon.css").body
	for _, want := range []string{
		`.cds-search-explain { min-width: 0; max-width: 100%;`,
		`.cds-search-explain > .cds-scroll-x > .cds-table { min-width: 46rem; }`,
	} {
		if !strings.Contains(stylesheet, want) {
			t.Fatalf("search explanation responsive style missing %q", want)
		}
	}
}
