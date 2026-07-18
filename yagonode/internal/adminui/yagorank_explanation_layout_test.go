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
