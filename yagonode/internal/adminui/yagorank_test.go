package adminui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeRanking struct {
	profile   RankingProfile
	tune      RankingTuneResult
	tuneErr   error
	applyErr  error
	applied   map[string]float64
	tuneCalls int
}

func (f *fakeRanking) Profile(context.Context) RankingProfile { return f.profile }

func (f *fakeRanking) Tune(context.Context) (RankingTuneResult, error) {
	f.tuneCalls++

	return f.tune, f.tuneErr
}

func (f *fakeRanking) Apply(_ context.Context, weights map[string]float64) error {
	f.applied = weights

	return f.applyErr
}

func sampleRankingProfile() RankingProfile {
	return RankingProfile{
		JudgmentCount:               3,
		JudgmentsAvailable:          true,
		TrainingReadinessAvailable:  true,
		ModelTrainingReady:          true,
		TrainingJudgmentCount:       120,
		TrainingQueryClusterCount:   120,
		HeldoutQueryClusterCount:    25,
		MinimumHeldoutQueryClusters: 20,
		Weights: []RankingWeight{
			{
				Key: "title", Label: "Title", Group: "Field boosts", Value: 6,
				Default: 6, Maximum: 64,
			},
			{
				Key: "body", Label: "Body", Group: "Field boosts", Value: 1,
				Default: 1, Maximum: 64,
			},
			{
				Key: "quality", Label: "Content quality", Group: "Priors", Value: 0.2,
				Default: 0.2, Maximum: 1,
			},
		},
	}
}

func TestConsoleYagoRankJudgmentsUnavailable(t *testing.T) {
	t.Parallel()

	profile := sampleRankingProfile()
	profile.JudgmentCount = 0
	profile.JudgmentsAvailable = false
	body := do(t, New(Options{Ranking: &fakeRanking{profile: profile}}), "/admin/yagorank").body
	if !strings.Contains(body, "Judgments: Unavailable") {
		t.Fatal("failed judgment read should render unavailable")
	}
	if strings.Contains(body, "0 judgments") {
		t.Fatal("failed judgment read rendered a fabricated zero count")
	}
	if !strings.Contains(body, `value="tune" disabled aria-describedby="judgment-status"`) {
		t.Fatal("tune action should be unavailable while judgment state is unknown")
	}
}

func TestConsoleYagoRankRendersProfile(t *testing.T) {
	t.Parallel()

	console := New(Options{Ranking: &fakeRanking{profile: sampleRankingProfile()}})
	got := do(t, console, "/admin/yagorank")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"YagoRank", "Field boosts", "Priors", "Content quality",
		`name="title"`, `value="6"`, `name="quality"`, `value="0.2"`,
		"3 judgments", `value="save"`, `value="tune"`,
		"Default 6; allowed 0–64.",
		"every operator-safe live scoring coefficient",
		"RM3 blend, RRF, remote calibration, MMR and per-site caps",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("profile page missing %q", want)
		}
	}
}

func TestConsoleYagoRankMarksActiveLegacyWeightOutsideWriteRange(t *testing.T) {
	profile := sampleRankingProfile()
	profile.Weights[0].Value = 65
	profile.Weights[0].OutOfRange = true
	body := do(
		t,
		New(Options{Ranking: &fakeRanking{profile: profile}}),
		"/admin/yagorank",
	).body
	if !strings.Contains(body, `name="title" value="65"`) || !strings.Contains(
		body,
		"The active legacy value is outside the allowed range",
	) {
		t.Fatalf("legacy range state missing: %s", body)
	}
}

func TestConsoleYagoRankUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	if got := do(t, console, "/admin/yagorank"); !strings.Contains(got.body, yagorankUnavailable) {
		t.Fatal("expected unavailable state without a ranking source")
	}
	// The action route also guards the missing source.
	if got := doPost(
		t,
		console,
		"/admin/yagorank",
		url.Values{"action": {"save"}},
	); !strings.Contains(
		got.body,
		yagorankUnavailable,
	) {
		t.Fatal("expected unavailable state on POST without a ranking source")
	}
}

func TestConsoleYagoRankSaveApplies(t *testing.T) {
	t.Parallel()

	ranking := &fakeRanking{profile: sampleRankingProfile()}
	console := New(Options{Ranking: ranking})
	got := doPost(t, console, "/admin/yagorank", url.Values{
		"action":  {"save"},
		"title":   {"7"},
		"body":    {"2"},
		"quality": {"0.5"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if ranking.applied["title"] != 7 || ranking.applied["body"] != 2 ||
		ranking.applied["quality"] != 0.5 {
		t.Fatalf("applied weights = %+v", ranking.applied)
	}
	if !strings.Contains(got.body, "Ranking profile saved.") {
		t.Fatal("save confirmation not shown")
	}
}

func TestConsoleYagoRankSaveRejectsNonNumber(t *testing.T) {
	t.Parallel()

	ranking := &fakeRanking{profile: sampleRankingProfile()}
	console := New(Options{Ranking: ranking})
	got := doPost(t, console, "/admin/yagorank", url.Values{
		"action":  {"save"},
		"title":   {"not-a-number"},
		"body":    {"2"},
		"quality": {"0.5"},
	})
	if ranking.applied != nil {
		t.Fatalf("Apply ran on invalid input: %+v", ranking.applied)
	}
	if !strings.Contains(got.body, "Enter a number for Title.") {
		t.Fatalf("parse error not shown: %s", got.body)
	}
}

func TestConsoleYagoRankSaveSurfacesApplyError(t *testing.T) {
	t.Parallel()

	ranking := &fakeRanking{profile: sampleRankingProfile(), applyErr: context.DeadlineExceeded}
	console := New(Options{Ranking: ranking})
	got := doPost(t, console, "/admin/yagorank", url.Values{
		"action":  {"save"},
		"title":   {"7"},
		"body":    {"2"},
		"quality": {"0.5"},
	})
	if !strings.Contains(got.body, "Save failed:") {
		t.Fatalf("apply error not shown: %s", got.body)
	}
}

func TestConsoleYagoRankTuneShowsPreview(t *testing.T) {
	t.Parallel()

	ranking := &fakeRanking{
		profile: sampleRankingProfile(),
		tune: RankingTuneResult{
			BeforeNDCG: 0.5000, AfterNDCG: 0.7000, Rounds: 3, Improved: true,
			Proposed: []RankingWeight{
				{Key: "title", Label: "Title", Group: "Field boosts", Value: 8},
				{Key: "body", Label: "Body", Group: "Field boosts", Value: 1},
				{Key: "quality", Label: "Content quality", Group: "Priors", Value: 0.3},
			},
		},
	}
	console := New(Options{Ranking: ranking})
	got := doPost(t, console, "/admin/yagorank", url.Values{"action": {"tune"}})
	if ranking.tuneCalls != 1 {
		t.Fatalf("Tune called %d times", ranking.tuneCalls)
	}
	for _, want := range []string{
		"Tuning preview", "0.5000", "0.7000", "lifted mean NDCG@10",
		// The proposed weights pre-fill the inputs so a Save applies them.
		`name="title"` + ` value="8"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("tune preview missing %q in %s", want, got.body)
		}
	}
}

func TestConsoleYagoRankTuneWithoutImprovement(t *testing.T) {
	t.Parallel()

	ranking := &fakeRanking{
		profile: sampleRankingProfile(),
		tune:    RankingTuneResult{BeforeNDCG: 0.6, AfterNDCG: 0.6, Rounds: 2, Improved: false},
	}
	console := New(Options{Ranking: ranking})
	got := doPost(t, console, "/admin/yagorank", url.Values{"action": {"tune"}})
	if !strings.Contains(got.body, "Tuning found no improvement") {
		t.Fatalf("no-improvement notice missing: %s", got.body)
	}
}

func TestConsoleYagoRankTuneSurfacesError(t *testing.T) {
	t.Parallel()

	ranking := &fakeRanking{profile: sampleRankingProfile(), tuneErr: context.DeadlineExceeded}
	console := New(Options{Ranking: ranking})
	got := doPost(t, console, "/admin/yagorank", url.Values{"action": {"tune"}})
	if !strings.Contains(got.body, "Tuning failed:") {
		t.Fatalf("tune error not shown: %s", got.body)
	}
}

func TestConsoleYagoRankUnknownActionReported(t *testing.T) {
	t.Parallel()

	console := New(Options{Ranking: &fakeRanking{profile: sampleRankingProfile()}})
	got := doPost(t, console, "/admin/yagorank", url.Values{"action": {"noop"}})
	if !strings.Contains(got.body, "Unknown action.") {
		t.Fatalf("unknown action not reported: %s", got.body)
	}
}
