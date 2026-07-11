package rankingtrain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func TestSplitCandidateDatasetsValidationAndLeakage(t *testing.T) {
	if _, err := splitCandidateDatasets(nil, searcheval.HoldoutSplitConfig{}); err == nil ||
		!strings.Contains(err.Error(), "split held-out judgments") {
		t.Fatalf("invalid split config error = %v", err)
	}
	if _, err := splitCandidateDatasets(
		[]queryDataset{{
			judgment: searcheval.CanonicalJudgment{Query: "only", QueryCluster: "only"},
		}},
		searcheval.DefaultHoldoutSplitConfig(),
	); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("empty partition error = %v", err)
	}

	shared := queryDataset{judgment: searcheval.CanonicalJudgment{
		Query:        "shared",
		QueryCluster: "shared cluster",
	}}
	leaked := datasetSplit{
		train:       []queryDataset{shared},
		development: []queryDataset{shared},
		test: []queryDataset{{judgment: searcheval.CanonicalJudgment{
			Query:        "test",
			QueryCluster: "test",
		}}},
	}
	if _, err := validatedDatasetSplit(leaked); err == nil ||
		!strings.Contains(err.Error(), "leaked from train to development") {
		t.Fatalf("leakage error = %v", err)
	}
	if err := validateNoQueryClusterLeakage(datasetSplit{
		train: []queryDataset{shared, shared},
	}); err != nil {
		t.Fatalf("same-partition cluster rejected: %v", err)
	}
}

func TestSplitCandidateDatasetsAddsChronologicalClustersToTest(t *testing.T) {
	datasets := make([]queryDataset, 40)
	for index := range datasets {
		query := fmt.Sprintf("query-%02d", index)
		datasets[index].judgment = searcheval.CanonicalJudgment{
			Query: query, QueryCluster: query,
		}
	}
	datasets[len(datasets)-1].judgment.ObservedAt = time.Unix(1_800_000_000, 0).UTC()
	config := searcheval.DefaultHoldoutSplitConfig()
	config.ChronologicalAfter = time.Unix(1_700_000_000, 0).UTC()
	config.ChronologicalFraction = 0
	split, err := splitCandidateDatasets(datasets, config)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, dataset := range split.test {
		found = found || dataset.judgment.Query == "query-39"
	}
	if !found {
		t.Fatalf("chronological query missing from test: %+v", split.test)
	}
}

func TestDatasetPartitionMappingAndCounts(t *testing.T) {
	first := queryDataset{
		judgment:        searcheval.CanonicalJudgment{Query: "first"},
		results:         make([]searchcore.Result, 2),
		modelCandidates: make([]modelCandidate, 1),
	}
	second := queryDataset{
		judgment:        searcheval.CanonicalJudgment{Query: "second"},
		results:         make([]searchcore.Result, 3),
		modelCandidates: make([]modelCandidate, 2),
	}
	mapped := datasetsForJudgments(
		[]searcheval.CanonicalJudgment{{Query: "second"}, {Query: "first"}},
		map[string]queryDataset{"first": first, "second": second},
	)
	if !reflect.DeepEqual(mapped, []queryDataset{second, first}) {
		t.Fatalf("mapped datasets = %+v", mapped)
	}
	counts := datasetCounts(datasetSplit{
		train:       []queryDataset{first},
		development: []queryDataset{second},
		test:        []queryDataset{first, second},
	})
	want := DatasetCounts{
		Train:       PartitionCounts{Queries: 1, Candidates: 2, ModelExamples: 1},
		Development: PartitionCounts{Queries: 1, Candidates: 3, ModelExamples: 2},
		Test:        PartitionCounts{Queries: 2, Candidates: 5, ModelExamples: 3},
	}
	if counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
}
