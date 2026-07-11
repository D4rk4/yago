package searcheval

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"time"
)

type HoldoutSplitConfig struct {
	TrainFraction         float64
	DevelopmentFraction   float64
	ChronologicalAfter    time.Time
	ChronologicalFraction float64
	Seed                  uint64
}

type HeldoutSplit struct {
	Train         []CanonicalJudgment
	Development   []CanonicalJudgment
	Test          []CanonicalJudgment
	Chronological []CanonicalJudgment
}

func DefaultHoldoutSplitConfig() HoldoutSplitConfig {
	return HoldoutSplitConfig{
		TrainFraction:         0.7,
		DevelopmentFraction:   0.15,
		ChronologicalFraction: 0.1,
		Seed:                  1,
	}
}

func SplitHeldoutJudgments(
	judgments []CanonicalJudgment,
	config HoldoutSplitConfig,
) (HeldoutSplit, error) {
	if err := validateHoldoutSplitConfig(config); err != nil {
		return HeldoutSplit{}, err
	}
	groups := make(map[string][]CanonicalJudgment)
	for _, judgment := range judgments {
		cluster := judgmentCluster(judgment)
		if cluster == "" {
			return HeldoutSplit{}, fmt.Errorf("judgment query cluster is empty")
		}
		groups[cluster] = append(groups[cluster], judgment)
	}
	chronological := chronologicalClusters(groups, config)
	keys := make([]string, 0, len(groups))
	for cluster := range groups {
		keys = append(keys, cluster)
	}
	sort.Strings(keys)
	var split HeldoutSplit
	for _, cluster := range keys {
		group := append([]CanonicalJudgment(nil), groups[cluster]...)
		sortJudgments(group)
		if chronological[cluster] {
			split.Chronological = append(split.Chronological, group...)

			continue
		}
		bucket := clusterBucket(cluster, config.Seed)
		switch {
		case bucket < config.TrainFraction:
			split.Train = append(split.Train, group...)
		case bucket < config.TrainFraction+config.DevelopmentFraction:
			split.Development = append(split.Development, group...)
		default:
			split.Test = append(split.Test, group...)
		}
	}

	return split, nil
}

func validateHoldoutSplitConfig(config HoldoutSplitConfig) error {
	if math.IsNaN(config.TrainFraction) || math.IsNaN(config.DevelopmentFraction) ||
		config.TrainFraction <= 0 || config.DevelopmentFraction < 0 ||
		config.TrainFraction+config.DevelopmentFraction >= 1 {
		return fmt.Errorf("train and development fractions must define a non-empty test split")
	}
	if math.IsNaN(config.ChronologicalFraction) || config.ChronologicalFraction < 0 ||
		config.ChronologicalFraction >= 1 {
		return fmt.Errorf("chronological fraction must be in [0,1)")
	}
	if !config.ChronologicalAfter.IsZero() && config.ChronologicalFraction > 0 {
		return fmt.Errorf("chronological cutoff and fraction are mutually exclusive")
	}

	return nil
}

func chronologicalClusters(
	groups map[string][]CanonicalJudgment,
	config HoldoutSplitConfig,
) map[string]bool {
	if !config.ChronologicalAfter.IsZero() {
		return chronologicalCutoffClusters(groups, config.ChronologicalAfter)
	}
	if config.ChronologicalFraction == 0 || len(groups) == 0 {
		return map[string]bool{}
	}

	return chronologicalFractionClusters(groups, config.ChronologicalFraction)
}

func chronologicalCutoffClusters(
	groups map[string][]CanonicalJudgment,
	cutoff time.Time,
) map[string]bool {
	selected := make(map[string]bool)
	for cluster, group := range groups {
		for _, judgment := range group {
			if !judgment.ObservedAt.IsZero() && !judgment.ObservedAt.Before(cutoff) {
				selected[cluster] = true

				break
			}
		}
	}

	return selected
}

func chronologicalFractionClusters(
	groups map[string][]CanonicalJudgment,
	fraction float64,
) map[string]bool {
	type datedCluster struct {
		cluster string
		latest  time.Time
	}
	dated := make([]datedCluster, 0, len(groups))
	for cluster, group := range groups {
		var latest time.Time
		for _, judgment := range group {
			if judgment.ObservedAt.After(latest) {
				latest = judgment.ObservedAt
			}
		}
		if !latest.IsZero() {
			dated = append(dated, datedCluster{cluster: cluster, latest: latest})
		}
	}
	if len(dated) == 0 {
		return map[string]bool{}
	}
	sort.Slice(dated, func(i, j int) bool {
		if !dated[i].latest.Equal(dated[j].latest) {
			return dated[i].latest.After(dated[j].latest)
		}

		return dated[i].cluster < dated[j].cluster
	})
	count := max(1, int(math.Ceil(float64(len(dated))*fraction)))
	selected := make(map[string]bool, count)
	for _, entry := range dated[:count] {
		selected[entry.cluster] = true
	}

	return selected
}

func clusterBucket(cluster string, seed uint64) float64 {
	hasher := fnv.New64a()
	var encodedSeed [8]byte
	binary.LittleEndian.PutUint64(encodedSeed[:], seed)
	_, _ = hasher.Write(encodedSeed[:])
	_, _ = hasher.Write([]byte(cluster))
	value := hasher.Sum64()
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	value ^= value >> 31

	return float64(value>>11) / float64(uint64(1)<<53)
}

func sortJudgments(judgments []CanonicalJudgment) {
	sort.Slice(judgments, func(i, j int) bool {
		if !judgments[i].ObservedAt.Equal(judgments[j].ObservedAt) {
			return judgments[i].ObservedAt.Before(judgments[j].ObservedAt)
		}

		return judgments[i].Query < judgments[j].Query
	})
}
