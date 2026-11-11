package worker

import (
	"testing"
	"time"
)

func TestSearchDemandRecrawlMinimumAge(t *testing.T) {
	now := time.Date(2026, time.November, 8, 15, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		focusedAt time.Time
		want      time.Duration
	}{
		{name: "ordinary demand", want: searchDemandRecrawlInterval},
		{name: "recent focus", focusedAt: now.Add(-time.Minute), want: time.Minute},
		{name: "old focus", focusedAt: now.Add(-searchDemandRecrawlInterval * 2), want: searchDemandRecrawlInterval},
		{name: "future focus", focusedAt: now.Add(time.Minute), want: searchDemandRecrawlInterval},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := searchDemandRecrawlMinimumAge(
				searchDemandCandidate{focusedAt: test.focusedAt},
				now,
			)
			if got != test.want {
				t.Fatalf("minimum age = %s, want %s", got, test.want)
			}
		})
	}
}

func TestMergeSearchDemandCandidatesKeepsStrongestScoreAndLatestFocus(t *testing.T) {
	firstFocus := time.Date(2026, time.November, 8, 14, 0, 0, 0, time.UTC)
	latestFocus := firstFocus.Add(time.Minute)
	merged := mergeSearchDemandCandidates(
		searchDemandCandidate{url: "https://example.test/", score: 4, focusedAt: latestFocus},
		searchDemandCandidate{url: "https://example.test/", score: 9, focusedAt: firstFocus},
	)
	if merged.score != 9 || !merged.focusedAt.Equal(latestFocus) {
		t.Fatalf("merged demand = %+v", merged)
	}
}
