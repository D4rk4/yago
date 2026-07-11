package rankingtrain

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

type datasetSplit struct {
	train       []queryDataset
	development []queryDataset
	test        []queryDataset
}

func splitCandidateDatasets(
	datasets []queryDataset,
	config searcheval.HoldoutSplitConfig,
) (datasetSplit, error) {
	judgments := make([]searcheval.CanonicalJudgment, len(datasets))
	byQuery := make(map[string]queryDataset, len(datasets))
	for index, dataset := range datasets {
		judgments[index] = dataset.judgment
		byQuery[dataset.judgment.Query] = dataset
	}
	split, err := searcheval.SplitHeldoutJudgments(judgments, config)
	if err != nil {
		return datasetSplit{}, fmt.Errorf("split held-out judgments: %w", err)
	}
	testJudgments := append(
		append([]searcheval.CanonicalJudgment(nil), split.Test...),
		split.Chronological...,
	)
	partitioned := datasetSplit{
		train:       datasetsForJudgments(split.Train, byQuery),
		development: datasetsForJudgments(split.Development, byQuery),
		test:        datasetsForJudgments(testJudgments, byQuery),
	}

	return validatedDatasetSplit(partitioned)
}

func validatedDatasetSplit(partitioned datasetSplit) (datasetSplit, error) {
	if len(partitioned.train) == 0 || len(partitioned.development) == 0 ||
		len(partitioned.test) == 0 {
		return datasetSplit{}, fmt.Errorf("train, development, and test splits must not be empty")
	}
	if err := validateNoQueryClusterLeakage(partitioned); err != nil {
		return datasetSplit{}, err
	}

	return partitioned, nil
}

func datasetsForJudgments(
	judgments []searcheval.CanonicalJudgment,
	byQuery map[string]queryDataset,
) []queryDataset {
	datasets := make([]queryDataset, len(judgments))
	for index, judgment := range judgments {
		datasets[index] = byQuery[judgment.Query]
	}

	return datasets
}

func validateNoQueryClusterLeakage(split datasetSplit) error {
	owners := make(map[string]string)
	partitions := []struct {
		name     string
		datasets []queryDataset
	}{
		{name: "train", datasets: split.train},
		{name: "development", datasets: split.development},
		{name: "test", datasets: split.test},
	}
	for _, partition := range partitions {
		for _, dataset := range partition.datasets {
			cluster := dataset.judgment.QueryCluster
			if owner := owners[cluster]; owner != "" && owner != partition.name {
				return fmt.Errorf(
					"query cluster %q leaked from %s to %s",
					cluster,
					owner,
					partition.name,
				)
			}
			owners[cluster] = partition.name
		}
	}

	return nil
}

func datasetCounts(split datasetSplit) DatasetCounts {
	return DatasetCounts{
		Train:       partitionCounts(split.train),
		Development: partitionCounts(split.development),
		Test:        partitionCounts(split.test),
	}
}

func partitionCounts(datasets []queryDataset) PartitionCounts {
	counts := PartitionCounts{Queries: len(datasets)}
	for _, dataset := range datasets {
		counts.Candidates += len(dataset.results)
		counts.ModelExamples += len(dataset.modelCandidates)
	}

	return counts
}
