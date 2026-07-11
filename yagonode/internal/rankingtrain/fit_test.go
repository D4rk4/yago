package rankingtrain

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestTrainRankingModelRejectsMissingPreferenceEvidence(t *testing.T) {
	dataset, err := buildQueryDataset(
		gradedJudgment{query: "query", relevant: map[string]int{}},
		[]searchcore.Result{
			rankingFixtureResult("https://first.example", 2, 0),
			rankingFixtureResult("https://second.example", 1, 1),
		},
	)
	if err != nil {
		t.Fatalf("buildQueryDataset: %v", err)
	}
	if _, _, _, err := trainRankingModel(
		t.Context(),
		FamilyLinearLambdaRank,
		"revision",
		[]queryDataset{{}, dataset},
	); err == nil || !strings.Contains(err.Error(), "no preference evidence") {
		t.Fatalf("missing preference error = %v", err)
	}
}

func TestTrainRankingModelPropagatesCancellationAndSnapshotFailures(t *testing.T) {
	dataset := preferenceDataset(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := trainRankingModel(
		cancelled,
		FamilyLinearLambdaRank,
		"revision",
		[]queryDataset{dataset},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("count cancellation error = %v", err)
	}
	for _, family := range []ModelFamily{FamilyLinearLambdaRank, FamilyHistogramLambdaMART} {
		ctx := &callCancellationContext{cancelAt: 2}
		if _, _, _, err := trainRankingModel(
			ctx,
			family,
			"revision",
			[]queryDataset{dataset},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s training cancellation error = %v", family, err)
		}
		ctx = &callCancellationContext{cancelAt: 5}
		if _, _, _, err := trainRankingModel(
			ctx,
			family,
			"revision",
			[]queryDataset{dataset},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s learner cancellation error = %v", family, err)
		}
		if _, _, _, err := trainRankingModel(
			t.Context(),
			family,
			"!invalid",
			[]queryDataset{dataset},
		); err == nil || !strings.Contains(err.Error(), "snapshot") {
			t.Fatalf("%s snapshot error = %v", family, err)
		}
	}
	if _, _, _, err := trainRankingModel(
		t.Context(),
		ModelFamily("future"),
		"revision",
		[]queryDataset{dataset},
	); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported family error = %v", err)
	}
}

func TestPreferencePairCountingAndUnsupportedPrediction(t *testing.T) {
	dataset := preferenceDataset(t)
	pairs, err := countPreferencePairs(t.Context(), []rankfit.QueryGroup{dataset.group})
	if err != nil || pairs != 3 {
		t.Fatalf("preference pairs = %d, %v", pairs, err)
	}
	for _, family := range []ModelFamily{FamilyLinearLambdaRank, FamilyHistogramLambdaMART} {
		if _, err := (trainedRankingModel{family: family}).predict(dataset.group); err == nil {
			t.Fatalf("invalid %s model predicted", family)
		}
	}
	if _, err := (trainedRankingModel{
		family: ModelFamily("future"),
	}).predict(dataset.group); err == nil {
		t.Fatal("unsupported model family predicted")
	}
	for _, family := range []ModelFamily{FamilyLinearLambdaRank, FamilyHistogramLambdaMART} {
		_, snapshot, _, err := trainRankingModel(
			t.Context(),
			family,
			"conversion",
			[]queryDataset{dataset},
		)
		if err != nil {
			t.Fatalf("train %s: %v", family, err)
		}
		converted, err := trainedRankingModelForSnapshot(snapshot)
		if err != nil || converted.family != family {
			t.Fatalf("convert %s = %+v, %v", family, converted, err)
		}
	}
	if _, err := trainedRankingModelForSnapshot(learnedrank.Snapshot{}); err == nil {
		t.Fatal("empty snapshot was converted")
	}
}

func TestPreferencePairCountingBoundsAndInnerCancellation(t *testing.T) {
	vector, err := rankfit.NewFeatureVector([]float64{0})
	if err != nil {
		t.Fatal(err)
	}
	examples := make([]rankfit.RankingExample, 2048)
	for index := range examples {
		relevance := 0
		if index >= len(examples)/2 {
			relevance = 1
		}
		examples[index], err = rankfit.NewRankingExample(
			strconv.Itoa(index),
			relevance,
			vector,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	group, err := rankfit.NewQueryGroup("bounded", examples)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := countPreferencePairs(t.Context(), []rankfit.QueryGroup{group}); err == nil {
		t.Fatal("preference pair overflow was accepted")
	}
	if _, err := countPreferencePairs(
		&callCancellationContext{cancelAt: 3},
		[]rankfit.QueryGroup{group},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("inner pair cancellation = %v", err)
	}
}

func preferenceDataset(t *testing.T) queryDataset {
	t.Helper()
	dataset, err := buildQueryDataset(
		gradedJudgment{
			query: "query",
			relevant: map[string]int{
				"https://middle.example": 1,
				"https://good.example":   3,
			},
		},
		[]searchcore.Result{
			rankingFixtureResult("https://bad.example", 3, 0),
			rankingFixtureResult("https://middle.example", 2, 0.5),
			rankingFixtureResult("https://good.example", 1, 1),
		},
	)
	if err != nil {
		t.Fatalf("buildQueryDataset: %v", err)
	}

	return dataset
}

type callCancellationContext struct {
	cancelAt int
	calls    int
}

func (*callCancellationContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*callCancellationContext) Done() <-chan struct{} {
	return nil
}

func (c *callCancellationContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}

	return nil
}

func (*callCancellationContext) Value(any) any {
	return nil
}
