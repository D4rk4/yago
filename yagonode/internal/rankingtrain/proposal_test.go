package rankingtrain

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func TestBuildLinearProposalIsDeterministicAndImmutable(t *testing.T) {
	judgments, firstSearcher := rankingFixture()
	config := Config{Revision: "linear-v1"}
	first, err := BuildProposal(t.Context(), firstSearcher, judgments, config)
	if err != nil {
		t.Fatalf("BuildProposal first: %v", err)
	}
	_, secondSearcher := rankingFixture()
	second, err := BuildProposal(t.Context(), secondSearcher, judgments, config)
	if err != nil {
		t.Fatalf("BuildProposal second: %v", err)
	}
	assertTrainingRequests(t, firstSearcher.requests, len(judgments))
	assertTrainingRequests(t, secondSearcher.requests, len(judgments))
	firstJSON, err := json.Marshal(first.Snapshot())
	if err != nil {
		t.Fatalf("Marshal first snapshot: %v", err)
	}
	secondJSON, err := json.Marshal(second.Snapshot())
	if err != nil {
		t.Fatalf("Marshal second snapshot: %v", err)
	}
	if !reflect.DeepEqual(firstJSON, secondJSON) || first.Snapshot().Revision() != "linear-v1" ||
		first.Snapshot().Kind() != learnedrank.ModelLinearLambdaRank {
		t.Fatalf("snapshots differ: %s %s", firstJSON, secondJSON)
	}
	if first.Counts() != second.Counts() || first.TrainingReport() != second.TrainingReport() {
		t.Fatalf("deterministic metadata differs: %+v %+v", first.Counts(), second.Counts())
	}
	assertSuccessfulProposal(t, first, len(judgments), FamilyLinearLambdaRank)

	development := first.DevelopmentEvaluation()
	development.Candidate.Slices["changed"] = searcheval.MetricSet{Queries: 99}
	development.Candidate.Queries[0].SliceNames = append(
		development.Candidate.Queries[0].SliceNames,
		"changed",
	)
	if len(first.DevelopmentEvaluation().Candidate.Slices) != 0 ||
		len(first.DevelopmentEvaluation().Candidate.Queries[0].SliceNames) != 0 {
		t.Fatal("development report changed through caller data")
	}
	decision := first.Decision()
	decision.Reasons = append(decision.Reasons, "changed")
	if len(first.Decision().Reasons) != 0 {
		t.Fatal("promotion decision changed through caller data")
	}
}

func TestBuildHistogramProposal(t *testing.T) {
	judgments, searcher := rankingFixture()
	config := DefaultConfig("histogram-v1", FamilyHistogramLambdaMART)
	proposal, err := BuildProposal(t.Context(), searcher, judgments, config)
	if err != nil {
		t.Fatalf("BuildProposal: %v", err)
	}
	if proposal.Snapshot().Kind() != learnedrank.ModelHistogramLambdaMART {
		t.Fatalf("snapshot kind = %q", proposal.Snapshot().Kind())
	}
	model, found := proposal.Snapshot().HistogramModel()
	if !found || model.TreeCount() == 0 {
		t.Fatalf("histogram model = %#v, %v", model, found)
	}
	assertSuccessfulProposal(t, proposal, len(judgments), FamilyHistogramLambdaMART)
}

func TestBuildProposalRequiresGainOverFrozenIncumbent(t *testing.T) {
	judgments, firstSearcher := rankingFixture()
	first, err := BuildProposal(
		t.Context(),
		firstSearcher,
		judgments,
		DefaultConfig("first", FamilyLinearLambdaRank),
	)
	if err != nil {
		t.Fatalf("BuildProposal first: %v", err)
	}
	incumbent := first.Snapshot()
	_, secondSearcher := rankingFixture()
	config := DefaultConfig("second", FamilyLinearLambdaRank)
	config.Incumbent = &incumbent
	second, err := BuildProposal(t.Context(), secondSearcher, judgments, config)
	if err != nil {
		t.Fatalf("BuildProposal second: %v", err)
	}
	test := second.TestEvaluation()
	if test.Incumbent == nil || second.Decision().IncumbentConfidence == nil ||
		second.Decision().Promote ||
		test.Candidate.Metrics.NDCGAt10 != test.Incumbent.Metrics.NDCGAt10 {
		t.Fatalf("incumbent comparison = %+v, %+v", test, second.Decision())
	}
	test.Incumbent.Queries[0].SliceNames = append(
		test.Incumbent.Queries[0].SliceNames,
		"changed",
	)
	decision := second.Decision()
	decision.IncumbentConfidence.ObservedRelativeGain = 99
	if len(second.TestEvaluation().Incumbent.Queries[0].SliceNames) != 0 ||
		second.Decision().IncumbentConfidence.ObservedRelativeGain == 99 {
		t.Fatal("incumbent evaluation aliases proposal state")
	}
}

func TestProposalReportClonesSlices(t *testing.T) {
	report := searcheval.EvaluationReport{
		Slices: map[string]searcheval.MetricSet{
			"slice": {Queries: 1},
		},
		Queries: []searcheval.QueryMetrics{{
			ID:         "query",
			SliceNames: []string{"slice"},
		}},
	}
	cloned := cloneEvaluationReport(report)
	cloned.Slices["slice"] = searcheval.MetricSet{Queries: 2}
	cloned.Queries[0].SliceNames[0] = "changed"
	if report.Slices["slice"].Queries != 1 || report.Queries[0].SliceNames[0] != "slice" {
		t.Fatalf("report changed through clone: %+v", report)
	}
}

func assertSuccessfulProposal(
	t *testing.T,
	proposal Proposal,
	queryTotal int,
	family ModelFamily,
) {
	t.Helper()
	counts := proposal.Counts()
	if counts.Train.Queries == 0 || counts.Development.Queries == 0 || counts.Test.Queries == 0 ||
		counts.Train.Queries+counts.Development.Queries+counts.Test.Queries != queryTotal {
		t.Fatalf("counts = %+v", counts)
	}
	for _, partition := range []PartitionCounts{counts.Train, counts.Development, counts.Test} {
		if partition.Candidates != partition.Queries*3 ||
			partition.ModelExamples != partition.Queries*3 {
			t.Fatalf("partition counts = %+v", partition)
		}
	}
	report := proposal.TrainingReport()
	if report.Family != family || report.PreferencePairs == 0 {
		t.Fatalf("training report = %+v", report)
	}
	if family == FamilyLinearLambdaRank && (report.Iterations == 0 || report.Trees != 0) {
		t.Fatalf("linear training report = %+v", report)
	}
	if family == FamilyHistogramLambdaMART && (report.Trees == 0 || report.Iterations != 0) {
		t.Fatalf("histogram training report = %+v", report)
	}
	for _, comparison := range []EvaluationComparison{
		proposal.DevelopmentEvaluation(),
		proposal.TestEvaluation(),
	} {
		if comparison.Candidate.Metrics.NDCGAt10 <= comparison.Baseline.Metrics.NDCGAt10 {
			t.Fatalf("evaluation did not improve: %+v", comparison)
		}
	}
	if decision := proposal.Decision(); !decision.Promote || len(decision.Reasons) != 0 {
		t.Fatalf("promotion decision = %+v", decision)
	}
}
