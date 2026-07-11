package rankingtrain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func TestEvaluateRankingModelHandlesNoopCancellationAndModelErrors(t *testing.T) {
	baseline := []searchcore.Result{{
		URL:   "https://only.example",
		Score: 1,
	}}
	unchanged, err := modelRankedResults(
		trainedRankingModel{},
		queryDataset{results: baseline},
	)
	if err != nil || len(unchanged) != 1 || unchanged[0].URL != baseline[0].URL ||
		unchanged[0].Score != baseline[0].Score {
		t.Fatalf("no-op ranking = %+v, %v", unchanged, err)
	}
	baseline[0].Score = 9
	if unchanged[0].Score == 9 {
		t.Fatal("ranked candidates alias the baseline")
	}

	dataset := preferenceDataset(t)
	if _, err := modelRankedResults(
		trainedRankingModel{family: ModelFamily("future")},
		dataset,
	); err == nil {
		t.Fatal("unsupported model family ranked candidates")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := evaluateRankingModel(
		cancelled,
		trainedRankingModel{},
		nil,
		[]queryDataset{{}},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("evaluation cancellation error = %v", err)
	}
	if _, err := evaluateRankingModel(
		t.Context(),
		trainedRankingModel{family: ModelFamily("future")},
		nil,
		[]queryDataset{dataset},
	); err == nil || !strings.Contains(err.Error(), "rank query") {
		t.Fatalf("ranking error = %v", err)
	}
}

func TestEvaluateRankingModelPropagatesCanonicalEvaluationErrors(t *testing.T) {
	if _, err := evaluateRankingModel(
		t.Context(),
		trainedRankingModel{},
		nil,
		[]queryDataset{{}},
	); err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("baseline evaluation error = %v", err)
	}

	dataset := preferenceDataset(t)
	model, _, _, err := trainRankingModel(
		t.Context(),
		FamilyLinearLambdaRank,
		"revision",
		[]queryDataset{dataset},
	)
	if err != nil {
		t.Fatalf("trainRankingModel: %v", err)
	}
	for index := range dataset.modelCandidates {
		dataset.modelCandidates[index].identity = "mismatch"
	}
	if _, err := evaluateRankingModel(
		t.Context(),
		model,
		nil,
		[]queryDataset{dataset},
	); err == nil || !strings.Contains(err.Error(), "candidate") {
		t.Fatalf("candidate evaluation error = %v", err)
	}
	incumbent := trainedRankingModel{}
	if _, err := evaluateRankingModel(
		t.Context(),
		trainedRankingModel{},
		&incumbent,
		[]queryDataset{{}},
	); err == nil || !strings.Contains(err.Error(), "active incumbent") {
		t.Fatalf("incumbent evaluation error = %v", err)
	}
	invalidIncumbent := trainedRankingModel{family: ModelFamily("future")}
	dataset = preferenceDataset(t)
	if _, err := evaluateRankingModel(
		t.Context(),
		model,
		&invalidIncumbent,
		[]queryDataset{dataset},
	); err == nil || !strings.Contains(err.Error(), "with active incumbent") {
		t.Fatalf("incumbent ranking error = %v", err)
	}
}

func TestEvaluateRankingModelAppliesFinalHostCrowdingToBothArms(t *testing.T) {
	results := make([]searchcore.Result, 12)
	for index := range results {
		results[index] = searchcore.Result{
			URL:   fmt.Sprintf("https://crowded.example/%d", index),
			Host:  "crowded.example",
			Score: float64(len(results) - index),
		}
	}
	results[len(results)-1].URL = "https://other.example/document"
	results[len(results)-1].Host = "other.example"
	comparison, err := evaluateRankingModel(
		t.Context(),
		trainedRankingModel{},
		nil,
		[]queryDataset{{
			judgment: searcheval.CanonicalJudgment{
				Query:            "crowded",
				QueryCluster:     "crowded",
				RelevantClusters: map[string]int{},
			},
			request: searchcore.Request{Query: "crowded"},
			results: results,
		}},
	)
	if err != nil {
		t.Fatalf("evaluateRankingModel: %v", err)
	}
	if comparison.Baseline.Metrics.UniqueRegistrableDomainCoverage != 0.2 ||
		comparison.Candidate.Metrics.UniqueRegistrableDomainCoverage != 0.2 {
		t.Fatalf("diversity metrics = %+v", comparison)
	}
}

func TestEvaluateRankingModelMeasuresBothArms(t *testing.T) {
	times := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, int64(time.Millisecond)),
		time.Unix(0, int64(2*time.Millisecond)),
		time.Unix(0, int64(5*time.Millisecond)),
	}
	index := 0
	dataset := queryDataset{
		judgment: searcheval.CanonicalJudgment{
			Query: "query", QueryCluster: "query", RelevantClusters: map[string]int{},
		},
		request: searchcore.Request{Query: "query"},
		results: []searchcore.Result{{URL: "https://example.test/", Score: 1}},
	}
	comparison, err := evaluateRankingModel(
		t.Context(),
		trainedRankingModel{},
		nil,
		[]queryDataset{dataset},
		func() time.Time {
			at := times[index]
			index++

			return at
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Baseline.Metrics.CPULatencyP95 != time.Millisecond ||
		comparison.Candidate.Metrics.CPULatencyP95 != 3*time.Millisecond {
		t.Fatalf("latencies = %+v", comparison)
	}
}
