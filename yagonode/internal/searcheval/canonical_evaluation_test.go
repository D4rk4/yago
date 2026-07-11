package searcheval

import (
	"math"
	"testing"
)

func TestPoolCandidatesUsesCanonicalClustersDeterministically(t *testing.T) {
	rankings := [][]RankedCandidate{
		{
			{URL: "z", CanonicalCluster: "c1", Score: 1, Unsafe: true},
			{URL: "fallback", Score: math.NaN()},
			{URL: "", Score: 9},
			{URL: "infinite", CanonicalCluster: "top", Score: math.Inf(1)},
		},
		{
			{URL: "a", CanonicalCluster: "c1", Score: 2, Spam: true},
			{URL: "lower", CanonicalCluster: "c1", Score: 0},
			{URL: "b", CanonicalCluster: "c2", Score: 1},
			{URL: "a", CanonicalCluster: "c2", Score: 1},
			{URL: "same-score", CanonicalCluster: "c3", Score: 1},
			{URL: "fallback", Score: 0, Spam: true},
		},
	}
	got := PoolCandidates(rankings...)
	if len(got) != 5 {
		t.Fatalf("pooled = %+v", got)
	}
	if got[0].CanonicalCluster != "top" || got[1].CanonicalCluster != "c1" ||
		got[1].URL != "a" || !got[1].Unsafe || !got[1].Spam ||
		got[2].CanonicalCluster != "c2" || got[2].URL != "a" ||
		got[3].CanonicalCluster != "c3" ||
		got[4].CanonicalCluster != "fallback" || !got[4].Spam {
		t.Fatalf("pooled order = %+v", got)
	}
	again := PoolCandidates(rankings...)
	for index := range got {
		if got[index] != again[index] {
			t.Fatalf("pool is not deterministic: %+v %+v", got, again)
		}
	}
}

func TestCanonicalEvaluationFallbacks(t *testing.T) {
	judgment := CanonicalJudgment{
		Query:            "  Alpha   Query ",
		RelevantClusters: map[string]int{"url": 50},
	}
	if got := judgmentCluster(judgment); got != "alpha query" {
		t.Fatalf("cluster = %q", got)
	}
	judgment.QueryCluster = " Cluster One "
	if got := judgmentCluster(judgment); got != "cluster one" {
		t.Fatalf("explicit cluster = %q", got)
	}
	observation := QueryObservation{Judgment: judgment}
	if got := observationID(observation); got != "cluster one" {
		t.Fatalf("observation id = %q", got)
	}
	observation.ID = " explicit "
	if got := observationID(observation); got != "explicit" {
		t.Fatalf("explicit id = %q", got)
	}
	candidate := RankedCandidate{URL: "url"}
	if candidateCluster(candidate) != "url" || candidateGrade(judgment, candidate) != 30 {
		t.Fatalf("candidate fallback failed")
	}
	if !candidatePrecedes(
		RankedCandidate{URL: "a", Score: 2},
		RankedCandidate{URL: "b", Score: 1},
	) {
		t.Fatal("higher score did not precede")
	}
}
