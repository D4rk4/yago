package rankingtrain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func TestCanonicalGradedJudgmentsValidationAndOrdering(t *testing.T) {
	invalid := []struct {
		name      string
		judgments []searcheval.Judgment
	}{
		{name: "empty"},
		{name: "too many", judgments: make([]searcheval.Judgment, MaximumJudgments+1)},
		{name: "blank query", judgments: []searcheval.Judgment{{Query: " \t"}}},
		{
			name: "empty URL",
			judgments: []searcheval.Judgment{{
				Query:    "query",
				Relevant: map[string]int{"": 1},
			}},
		},
		{
			name: "negative grade",
			judgments: []searcheval.Judgment{{
				Query:    "query",
				Relevant: map[string]int{"https://example.test": -1},
			}},
		},
		{
			name: "excessive grade",
			judgments: []searcheval.Judgment{{
				Query:    "query",
				Relevant: map[string]int{"https://example.test": 31},
			}},
		},
		{
			name: "duplicate query",
			judgments: []searcheval.Judgment{
				{Query: "query"},
				{Query: " query "},
			},
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := canonicalGradedJudgments(test.judgments); err == nil {
				t.Fatal("invalid judgments accepted")
			}
		})
	}

	relevant := map[string]int{
		"https://z.example": 1,
		"https://a.example": 3,
	}
	intents := map[string][]string{"https://a.example": {"reference"}}
	slices := []string{"technical"}
	got, err := canonicalGradedJudgments([]searcheval.Judgment{
		{
			Query: " z query ", QueryCluster: " Z Family ", Relevant: relevant,
			ClusterIntents: intents, Navigational: true, SliceNames: slices,
		},
		{Query: "a query", Relevant: map[string]int{}},
	})
	if err != nil {
		t.Fatalf("canonicalGradedJudgments: %v", err)
	}
	relevant["https://a.example"] = 0
	intents["https://a.example"][0] = "changed"
	slices[0] = "changed"
	if got[0].query != "a query" || got[1].query != "z query" ||
		got[1].queryCluster != "z family" ||
		got[1].relevant["https://a.example"] != 3 ||
		got[1].clusterIntents["https://a.example"][0] != "reference" ||
		got[1].sliceNames[0] != "technical" || !got[1].navigational {
		t.Fatalf("canonical judgments = %+v", got)
	}
	sentinel := errors.New("mapping failed")
	if _, err := canonicalGradedJudgmentsWithMapper(
		[]searcheval.Judgment{{Query: "query"}},
		func(searchcore.RankingEvidence) (rankfit.FeatureVector, bool, error) {
			return rankfit.FeatureVector{}, false, sentinel
		},
	); !errors.Is(err, sentinel) {
		t.Fatalf("mapper error = %v", err)
	}
}

func TestRetrieveCandidateDatasetsPropagatesFailuresAndCancellation(t *testing.T) {
	if _, err := retrieveCandidateDatasets(
		t.Context(),
		&scriptedSearcher{},
		nil,
	); err == nil {
		t.Fatal("invalid judgments accepted")
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := retrieveCandidateDatasets(
		cancelled,
		&scriptedSearcher{},
		[]searcheval.Judgment{{Query: "query"}},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}

	failure := errors.New("search failed")
	if _, err := retrieveCandidateDatasets(
		t.Context(),
		&scriptedSearcher{failure: failure},
		[]searcheval.Judgment{{Query: "query"}},
	); !errors.Is(err, failure) {
		t.Fatalf("search error = %v", err)
	}

	ctx, cancelAfterFirst := context.WithCancel(t.Context())
	searcher := &scriptedSearcher{
		results: map[string][]searchcore.Result{
			"a": {{URL: "https://a.example"}},
			"b": {{URL: "https://b.example"}},
		},
		afterSearch: cancelAfterFirst,
	}
	if _, err := retrieveCandidateDatasets(ctx, searcher, []searcheval.Judgment{
		{Query: "b"},
		{Query: "a"},
	}); !errors.Is(err, context.Canceled) || len(searcher.requests) != 1 ||
		searcher.requests[0].Query != "a" {
		t.Fatalf("mid-retrieval cancellation = %v, %+v", err, searcher.requests)
	}

	if _, err := retrieveCandidateDatasets(
		t.Context(),
		&scriptedSearcher{results: map[string][]searchcore.Result{"query": {{}}}},
		[]searcheval.Judgment{{Query: "query"}},
	); err == nil || !strings.Contains(err.Error(), "build query") {
		t.Fatalf("candidate error = %v", err)
	}
	tooMany := make([]searcheval.Judgment, MaximumTrainingQueries+1)
	for index := range tooMany {
		tooMany[index].Query = fmt.Sprintf("bounded query %d", index)
	}
	boundedSearcher := &scriptedSearcher{}
	if _, err := retrieveCandidateDatasets(
		t.Context(),
		boundedSearcher,
		tooMany,
	); err == nil || len(boundedSearcher.requests) != 0 {
		t.Fatalf("oversized training pool = %v, %d", err, len(boundedSearcher.requests))
	}
	if MaximumTrainingQueries*MaximumCandidatesPerQuery != MaximumCandidatePool ||
		MaximumTrainingQueries*ModelCandidateWindow != MaximumModelExamples {
		t.Fatal("training query limit does not bound candidate memory")
	}
}

func TestBuildQueryDatasetUsesExactBoundedIdentities(t *testing.T) {
	results := make([]searchcore.Result, MaximumCandidatesPerQuery+5)
	for index := range results {
		url := fmt.Sprintf("https://result-%03d.example/document", index)
		results[index] = rankingFixtureResult(url, float64(len(results)-index), float64(index))
	}
	results[0].ClusterID = "shared-cluster"
	results[0].RepresentativeURL = "https://representative.example/document"
	results[0].Host = "result.example"
	results[0].SafetyRating = searchcore.SafetyExplicit
	results[0].SpamRisk = 1
	results[1].ClusterID = "shared-cluster"
	results[2].Evidence = searchcore.RankingEvidence{}
	judgment := gradedJudgment{
		query:        "  Exact   Query ",
		queryCluster: "exact family",
		observedAt:   time.Unix(1_800_000_000, 0).UTC(),
		relevant: map[string]int{
			results[0].URL: 1,
			"https://representative.example/document": 3,
			"https://absent.example/document":         2,
		},
		clusterIntents: map[string][]string{
			results[0].URL:                    {"shared"},
			"cluster:declared":                {"declared"},
			"https://absent.example/document": {"absent"},
		},
		navigational: true,
		sliceNames:   []string{"technical"},
	}
	dataset, err := buildQueryDataset(judgment, results)
	if err != nil {
		t.Fatalf("buildQueryDataset: %v", err)
	}
	if len(dataset.results) != MaximumCandidatesPerQuery-1 ||
		len(dataset.modelCandidates) != ModelCandidateWindow-1 || !dataset.hasGroup {
		t.Fatalf(
			"dataset sizes = %d, %d, %v",
			len(dataset.results),
			len(dataset.modelCandidates),
			dataset.hasGroup,
		)
	}
	if dataset.judgment.QueryCluster != "exact family" ||
		dataset.judgment.RelevantClusters["cluster:shared-cluster"] != 3 ||
		dataset.judgment.RelevantClusters["url:https://absent.example/document"] != 2 ||
		dataset.judgment.ClusterIntents["cluster:shared-cluster"][0] != "shared" ||
		dataset.judgment.ClusterIntents["cluster:declared"][0] != "declared" ||
		dataset.judgment.ClusterIntents["url:https://absent.example/document"][0] != "absent" ||
		!dataset.judgment.Navigational || dataset.judgment.SliceNames[0] != "technical" ||
		!dataset.judgment.ObservedAt.Equal(time.Unix(1_800_000_000, 0).UTC()) {
		t.Fatalf("canonical judgment = %+v", dataset.judgment)
	}
	first := canonicalRankedCandidates(dataset.results)[0]
	if first.CanonicalCluster != "cluster:shared-cluster" || first.URL != results[0].URL ||
		first.RegistrableDomain != "result.example" || !first.Unsafe || !first.Spam {
		t.Fatalf("first candidate = %+v", first)
	}
	if dataset.results[len(dataset.results)-1].URL != results[MaximumCandidatesPerQuery-1].URL {
		t.Fatalf("candidate limit not enforced: %+v", dataset.results[len(dataset.results)-1])
	}
	examples := dataset.group.Examples()
	if len(examples) != len(dataset.modelCandidates) ||
		examples[0].DocumentIdentifier() != "cluster:shared-cluster" ||
		examples[0].Relevance() != 3 {
		t.Fatalf("training examples = %+v", examples)
	}

	withoutEvidence, err := buildQueryDataset(
		gradedJudgment{query: "query", relevant: map[string]int{}},
		[]searchcore.Result{{URL: "https://unknown.example"}},
	)
	if err != nil || withoutEvidence.hasGroup || len(withoutEvidence.modelCandidates) != 0 {
		t.Fatalf("evidence-free dataset = %+v, %v", withoutEvidence, err)
	}
}

func TestBuildQueryDatasetRejectsInvalidState(t *testing.T) {
	validResult := rankingFixtureResult("https://valid.example", 1, 1)
	invalid := []struct {
		name     string
		judgment gradedJudgment
		results  []searchcore.Result
	}{
		{
			name:     "missing identity",
			judgment: gradedJudgment{query: "query"},
			results:  []searchcore.Result{{}},
		},
		{
			name: "invalid evidence",
			judgment: gradedJudgment{
				query:    "query",
				relevant: map[string]int{},
			},
			results: []searchcore.Result{{
				URL: "https://invalid.example",
			}},
		},
		{
			name: "invalid grade",
			judgment: gradedJudgment{
				query:    "query",
				relevant: map[string]int{validResult.URL: 31},
			},
			results: []searchcore.Result{validResult},
		},
		{
			name: "empty group query",
			judgment: gradedJudgment{
				relevant: map[string]int{},
			},
			results: []searchcore.Result{validResult},
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.name == "invalid evidence" {
				_, err = buildQueryDatasetWithMapper(
					test.judgment,
					test.results,
					func(searchcore.RankingEvidence) (rankfit.FeatureVector, bool, error) {
						return rankfit.FeatureVector{}, false, errors.New("invalid evidence")
					},
				)
			} else {
				_, err = buildQueryDataset(test.judgment, test.results)
			}
			if err == nil {
				t.Fatal("invalid candidate state accepted")
			}
		})
	}
	if rankingCandidateIdentity(searchcore.Result{}) != "" ||
		rankingCandidateIdentity(searchcore.Result{URL: "url"}) != "url:url" ||
		rankingCandidateIdentity(searchcore.Result{URL: "url", ClusterID: "cluster"}) !=
			"cluster:cluster" {
		t.Fatal("candidate identity precedence is incorrect")
	}
}
