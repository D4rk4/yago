package searcheval

import (
	"math"
	"testing"
	"time"
)

func heldoutMetricObservations() []QueryObservation {
	first := QueryObservation{
		ID: "q1",
		Judgment: CanonicalJudgment{
			Query:            "first",
			QueryCluster:     "cluster first",
			RelevantClusters: map[string]int{"a": 1, "b": 3, "c": 2, "": 9},
			ClusterIntents: map[string][]string{
				"a": {" one ", "one", ""},
				"b": {"two"},
				"c": {"one", "two"},
			},
			Navigational: true,
			SliceNames:   []string{" Head ", "head", ""},
		},
		Candidates: []RankedCandidate{
			{URL: "b1", CanonicalCluster: "b", RegistrableDomain: "B.example"},
			{
				URL: "b2", CanonicalCluster: "b", RegistrableDomain: "b.example",
				Unsafe: true,
			},
			{
				URL: "a", CanonicalCluster: "a", RegistrableDomain: "a.example",
				Spam: true,
			},
			{URL: "x", CanonicalCluster: "x"},
			{URL: "c", CanonicalCluster: "c", RegistrableDomain: "c.example"},
		},
		PeerBytes: 100, PeerTimeouts: 1, CPULatency: 10 * time.Millisecond,
	}
	second := QueryObservation{
		ID: "q2",
		Judgment: CanonicalJudgment{
			Query: "second", SliceNames: []string{"plain"},
		},
		PeerBytes: 200, CPULatency: 20 * time.Millisecond,
	}
	third := QueryObservation{
		ID: "q3",
		Judgment: CanonicalJudgment{
			Query: "third", RelevantClusters: map[string]int{"missing": 2},
			Navigational: true, SliceNames: []string{"head"},
		},
		PeerBytes: 300, CPULatency: 100 * time.Millisecond,
	}

	return []QueryObservation{longRecallObservation(), third, first, second}
}

func longRecallObservation() QueryObservation {
	longCandidates := make([]RankedCandidate, 150)
	for index := range longCandidates {
		longCandidates[index] = RankedCandidate{
			URL:               "candidate-" + string(rune(index+1000)),
			CanonicalCluster:  "cluster-" + string(rune(index+1000)),
			RegistrableDomain: "long.example",
		}
	}
	longCandidates[0].CanonicalCluster = "early"
	longCandidates[149].CanonicalCluster = "late"
	longCandidates[149].Unsafe = true
	return QueryObservation{
		ID: "q4",
		Judgment: CanonicalJudgment{
			Query: "fourth", RelevantClusters: map[string]int{"early": 1, "late": 1},
			SliceNames: []string{"long"},
		},
		Candidates: longCandidates,
		PeerBytes:  400, PeerTimeouts: 2, CPULatency: 30 * time.Millisecond,
	}
}

func TestEvaluateHeldoutMeasuresCanonicalRankingAndOperations(t *testing.T) {
	report, err := EvaluateHeldout(heldoutMetricObservations())
	if err != nil {
		t.Fatalf("EvaluateHeldout: %v", err)
	}
	if report.Metrics.Queries != 4 || report.Metrics.PeerBytes != 1000 ||
		report.Metrics.PeerTimeouts != 3 ||
		report.Metrics.CPULatencyP50 != 20*time.Millisecond ||
		report.Metrics.CPULatencyP95 != 100*time.Millisecond {
		t.Fatalf("operations = %+v", report.Metrics)
	}
	if report.Queries[0].ID != "q1" || report.Queries[0].RecallAt100 != 1 ||
		report.Queries[0].RecallAt200 != 1 ||
		report.Queries[0].NavigationalReciprocalRank != 1 ||
		math.Abs(report.Queries[0].DuplicateClusterRateAt10-0.2) > 1e-12 ||
		math.Abs(report.Queries[0].UniqueRegistrableDomainCoverage-0.6) > 1e-12 ||
		report.Queries[0].UnsafeErrors != 1 || report.Queries[0].SpamErrors != 1 {
		t.Fatalf("first query = %+v", report.Queries[0])
	}
	if report.Queries[0].NDCGAt10 <= 0 || report.Queries[0].NDCGAt10 >= 1 ||
		report.Queries[0].ERRAt10 <= 0 || report.Queries[0].AlphaNDCGAt10 <= 0 ||
		report.Queries[0].AlphaNDCGAt10 > 1 || report.Queries[0].IntentCoverageAt10 != 1 ||
		report.Queries[0].UnsafeExposureAt10 <= 0 ||
		report.Queries[0].UnsafeExposureAt10 <= report.Queries[0].SpamExposureAt10 {
		t.Fatalf("ranking metrics = %+v", report.Queries[0])
	}
	if report.Queries[3].RecallAt100 != 0.5 || report.Queries[3].RecallAt200 != 1 ||
		report.Queries[3].UnsafeErrors != 1 {
		t.Fatalf("long query = %+v", report.Queries[3])
	}
	if report.Metrics.NavigationalMRR != 0.5 || len(report.Slices) != 3 ||
		report.Slices["head"].Queries != 2 || report.Slices["plain"].Queries != 1 {
		t.Fatalf("aggregates = %+v", report)
	}
}

func TestEvaluateHeldoutEmptyAndValidation(t *testing.T) {
	report, err := EvaluateHeldout(nil)
	if err != nil || report.Metrics.Queries != 0 || report.Metrics.CPULatencyP50 != 0 ||
		len(report.Slices) != 0 {
		t.Fatalf("empty report = %+v err=%v", report, err)
	}
	cases := []QueryObservation{
		{},
		{ID: "negative", PeerBytes: -1},
		{ID: "candidate", Candidates: []RankedCandidate{{}}},
	}
	for _, observation := range cases {
		if _, err := EvaluateHeldout([]QueryObservation{observation}); err == nil {
			t.Fatalf("observation accepted: %+v", observation)
		}
	}
	if _, err := EvaluateHeldout([]QueryObservation{{ID: "same"}, {ID: "same"}}); err == nil {
		t.Fatal("duplicate observation id accepted")
	}
}

func TestCanonicalMetricEdgeCases(t *testing.T) {
	judgment := CanonicalJudgment{
		RelevantClusters: map[string]int{"a": -2},
		ClusterIntents:   map[string][]string{"a": {"intent"}},
	}
	candidates := []RankedCandidate{{URL: "x", RegistrableDomain: " "}}
	if canonicalRecall(candidates, judgment, 10) != 0 ||
		canonicalNDCG(candidates, judgment, 10) != 0 ||
		canonicalERR(candidates, judgment, 10) != 0 ||
		canonicalReciprocalRank(candidates, judgment, 10) != 0 ||
		alphaNDCG(candidates, judgment, 10) != 0 ||
		intentCoverage(candidates, judgment, 10) != 0 ||
		registrableDomainCoverage(candidates, 10) != 0 {
		t.Fatal("empty relevance metrics were not neutral")
	}
	if duplicateClusterRate(nil, 10) != 0 || durationPercentile(nil, 0.5) != 0 {
		t.Fatal("empty diversity or latency was not neutral")
	}
	unsafe, spam := safetyErrors([]RankedCandidate{{Unsafe: true, Spam: true, URL: "x"}}, 1)
	if unsafe != 1 || spam != 1 ||
		discountedExposure(nil, func(RankedCandidate) bool { return true }) != 0 ||
		discountedExposure(
			[]RankedCandidate{{URL: "safe"}, {URL: "unsafe", Unsafe: true}},
			func(candidate RankedCandidate) bool { return candidate.Unsafe },
		) >= 0.5 {
		t.Fatalf("safety = %d %d", unsafe, spam)
	}
}
