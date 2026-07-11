package adminui

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

type fakeLearnedRanking struct {
	*fakeRanking
	model         LearnedModelView
	trainOutcome  LearnedModelTrainOutcome
	trainErr      error
	trainedKinds  []LearnedModelKind
	rolledBack    bool
	rollbackErr   error
	rollbackCalls int
}

func (f *fakeLearnedRanking) LearnedModel(context.Context) LearnedModelView {
	return f.model
}

func (f *fakeLearnedRanking) TrainLearnedModel(
	_ context.Context,
	kind LearnedModelKind,
) (LearnedModelTrainOutcome, error) {
	f.trainedKinds = append(f.trainedKinds, kind)

	return f.trainOutcome, f.trainErr
}

func (f *fakeLearnedRanking) RollbackLearnedModel(context.Context) (bool, error) {
	f.rollbackCalls++

	return f.rolledBack, f.rollbackErr
}

func TestConsoleYagoRankRendersLearnedModelStatusAndCommands(t *testing.T) {
	t.Parallel()

	ranking := &fakeLearnedRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
		model: LearnedModelView{
			ActiveRevision:    "rank-2026-07-11",
			ActiveKind:        LearnedModelLinearLambdaRank,
			RollbackAvailable: true,
		},
	}
	body := do(t, New(Options{Ranking: ranking}), "/admin/yagorank").body
	for _, want := range []string{
		"Learned model",
		"Linear LambdaRank",
		"rank-2026-07-11",
		"Available",
		`value="train-linear"`,
		`value="train-tree"`,
		`value="rollback-model"`,
		"Train linear model",
		"Train tree model",
		"Rollback model",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("learned model page missing %q", want)
		}
	}
	for _, unwanted := range []string{"The learned ranking profile", "cds-tile"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("learned model page contains %q", unwanted)
		}
	}
}

func TestConsoleYagoRankRendersInactiveLearnedModelControls(t *testing.T) {
	t.Parallel()

	profile := sampleRankingProfile()
	profile.JudgmentCount = 0
	ranking := &fakeLearnedRanking{fakeRanking: &fakeRanking{profile: profile}}
	body := do(t, New(Options{Ranking: ranking}), "/admin/yagorank").body
	for _, want := range []string{
		"Built-in",
		"None",
		"Unavailable",
		`value="train-linear" disabled`,
		`value="train-tree" disabled`,
		`value="rollback-model" disabled`,
		"0 judgments",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("inactive model page missing %q", want)
		}
	}
}

func TestLearnedModelKindLabel(t *testing.T) {
	t.Parallel()

	for kind, want := range map[LearnedModelKind]string{
		LearnedModelLinearLambdaRank:    "Linear LambdaRank",
		LearnedModelHistogramLambdaMART: "Histogram LambdaMART",
		"":                              "Built-in",
		"future":                        "future",
	} {
		if got := learnedModelKindLabel(kind); got != want {
			t.Fatalf("label for %q = %q, want %q", kind, got, want)
		}
	}
}

func TestConsoleYagoRankTrainsLearnedModels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		action   string
		kind     LearnedModelKind
		outcome  LearnedModelTrainOutcome
		expected []string
	}{
		{
			name:   "linear promoted",
			action: "train-linear",
			kind:   LearnedModelLinearLambdaRank,
			outcome: LearnedModelTrainOutcome{
				Promoted: true, HeldOutNDCGGain: 0.031, Confidence: 0.95,
				TrainQueryCount: 70, DevelopmentQueryCount: 15, TestQueryCount: 15,
			},
			expected: []string{
				"Ranking model promoted.", "Promoted", "&#43;0.0310", "0.9500",
				"Train 70 / Dev 15 / Test 15",
			},
		},
		{
			name:   "tree rejected",
			action: "train-tree",
			kind:   LearnedModelHistogramLambdaMART,
			outcome: LearnedModelTrainOutcome{
				HeldOutNDCGGain: -0.01, Confidence: 0.9,
				Reasons:         []string{"held-out gain below threshold", "<slice regression>"},
				TrainQueryCount: 14, DevelopmentQueryCount: 3, TestQueryCount: 3,
			},
			expected: []string{
				"Ranking model was not promoted.", "Not promoted", "-0.0100", "0.9000",
				"held-out gain below threshold", "&lt;slice regression&gt;",
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ranking := &fakeLearnedRanking{
				fakeRanking:  &fakeRanking{profile: sampleRankingProfile()},
				trainOutcome: test.outcome,
			}
			body := doPost(
				t,
				New(Options{Ranking: ranking}),
				"/admin/yagorank",
				url.Values{"action": {test.action}},
			).body
			if len(ranking.trainedKinds) != 1 || ranking.trainedKinds[0] != test.kind {
				t.Fatalf("trained kinds = %v, want %q", ranking.trainedKinds, test.kind)
			}
			for _, want := range test.expected {
				if !strings.Contains(body, want) {
					t.Fatalf("training outcome missing %q", want)
				}
			}
		})
	}
}

func TestConsoleYagoRankSurfacesLearnedModelTrainingError(t *testing.T) {
	t.Parallel()

	ranking := &fakeLearnedRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
		trainErr:    context.DeadlineExceeded,
	}
	body := doPost(
		t,
		New(Options{Ranking: ranking}),
		"/admin/yagorank",
		url.Values{"action": {"train-linear"}},
	).body
	if !strings.Contains(body, "Model training failed: context deadline exceeded") {
		t.Fatalf("training error missing: %s", body)
	}
}

func TestConsoleYagoRankRejectsLearnedActionsWithoutCapability(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"train-linear", "rollback-model"} {
		body := doPost(
			t,
			New(Options{Ranking: &fakeRanking{profile: sampleRankingProfile()}}),
			"/admin/yagorank",
			url.Values{"action": {action}},
		).body
		if !strings.Contains(body, "Learned model operations are not available.") {
			t.Fatalf("action %q did not report unavailable capability", action)
		}
	}
}

func TestConsoleYagoRankRollsBackLearnedModel(t *testing.T) {
	t.Parallel()

	ranking := &fakeLearnedRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
		rolledBack:  true,
	}
	body := doPost(
		t,
		New(Options{Ranking: ranking}),
		"/admin/yagorank",
		url.Values{"action": {"rollback-model"}},
	).body
	if ranking.rollbackCalls != 1 || !strings.Contains(body, "Ranking model rolled back.") {
		t.Fatalf("rollback result calls=%d body=%s", ranking.rollbackCalls, body)
	}
}

func TestConsoleYagoRankReportsEmptyRollbackHistory(t *testing.T) {
	t.Parallel()

	ranking := &fakeLearnedRanking{fakeRanking: &fakeRanking{profile: sampleRankingProfile()}}
	body := doPost(
		t,
		New(Options{Ranking: ranking}),
		"/admin/yagorank",
		url.Values{"action": {"rollback-model"}},
	).body
	if !strings.Contains(body, "No ranking model revision is available for rollback.") {
		t.Fatalf("empty rollback history missing: %s", body)
	}
}

func TestConsoleYagoRankSurfacesRollbackError(t *testing.T) {
	t.Parallel()

	ranking := &fakeLearnedRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
		rollbackErr: context.DeadlineExceeded,
	}
	body := doPost(
		t,
		New(Options{Ranking: ranking}),
		"/admin/yagorank",
		url.Values{"action": {"rollback-model"}},
	).body
	if !strings.Contains(body, "Model rollback failed: context deadline exceeded") {
		t.Fatalf("rollback error missing: %s", body)
	}
}
