package searcheval

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestSplitHeldoutJudgmentsIsClusteredChronologicalAndDeterministic(t *testing.T) {
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	judgments := []CanonicalJudgment{
		{Query: "future old", QueryCluster: "future", ObservedAt: cutoff.AddDate(-1, 0, 0)},
		{Query: "future new", QueryCluster: "future", ObservedAt: cutoff},
		{Query: "z", QueryCluster: "zeta", ObservedAt: cutoff.AddDate(-2, 0, 0)},
	}
	for index := 0; index < 100; index++ {
		judgments = append(judgments, CanonicalJudgment{
			Query:        "query",
			QueryCluster: fmt.Sprintf("cluster-%03d", index),
			ObservedAt:   cutoff.AddDate(-2, 0, 0),
		})
	}
	config := HoldoutSplitConfig{
		TrainFraction:       0.5,
		DevelopmentFraction: 0.25,
		ChronologicalAfter:  cutoff,
		Seed:                42,
	}
	first, err := SplitHeldoutJudgments(judgments, config)
	if err != nil {
		t.Fatalf("SplitHeldoutJudgments: %v", err)
	}
	second, err := SplitHeldoutJudgments(judgments, config)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("split is not deterministic: err=%v", err)
	}
	if len(first.Chronological) != 2 || first.Chronological[0].Query != "future old" ||
		len(first.Train) == 0 || len(first.Development) == 0 || len(first.Test) == 0 {
		t.Fatalf("split = %+v", first)
	}
	locations := map[string]string{}
	for name, partition := range map[string][]CanonicalJudgment{
		"train": first.Train, "development": first.Development,
		"test": first.Test, "chronological": first.Chronological,
	} {
		for _, judgment := range partition {
			cluster := judgmentCluster(judgment)
			if previous := locations[cluster]; previous != "" && previous != name {
				t.Fatalf("cluster %q leaked from %s to %s", cluster, previous, name)
			}
			locations[cluster] = name
		}
	}
}

func TestSplitHeldoutJudgmentsChronologicalFraction(t *testing.T) {
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	judgments := []CanonicalJudgment{
		{Query: "a", ObservedAt: base},
		{Query: "b", ObservedAt: base.Add(time.Hour)},
		{Query: "c", ObservedAt: base.Add(2 * time.Hour)},
		{Query: "d", ObservedAt: base.Add(2 * time.Hour)},
	}
	split, err := SplitHeldoutJudgments(judgments, HoldoutSplitConfig{
		TrainFraction:         0.6,
		DevelopmentFraction:   0.2,
		ChronologicalFraction: 0.25,
	})
	if err != nil {
		t.Fatalf("SplitHeldoutJudgments: %v", err)
	}
	if len(split.Chronological) != 1 || split.Chronological[0].Query != "c" {
		t.Fatalf("chronological = %+v", split.Chronological)
	}
	empty, err := SplitHeldoutJudgments(nil, HoldoutSplitConfig{
		TrainFraction:         0.6,
		DevelopmentFraction:   0.2,
		ChronologicalFraction: 0.25,
	})
	if err != nil || len(empty.Chronological) != 0 {
		t.Fatalf("empty split = %+v err=%v", empty, err)
	}
	defaults := DefaultHoldoutSplitConfig()
	if defaults.TrainFraction != 0.7 || defaults.DevelopmentFraction != 0.15 ||
		defaults.ChronologicalFraction != 0.1 || defaults.Seed != 1 {
		t.Fatalf("defaults = %+v", defaults)
	}
	ordered := []CanonicalJudgment{
		{Query: "b", QueryCluster: "same", ObservedAt: base},
		{Query: "a", QueryCluster: "same", ObservedAt: base},
	}
	sortJudgments(ordered)
	if ordered[0].Query != "a" {
		t.Fatalf("query tie order = %+v", ordered)
	}
	undated, err := SplitHeldoutJudgments([]CanonicalJudgment{{Query: "undated"}},
		HoldoutSplitConfig{
			TrainFraction:         0.6,
			DevelopmentFraction:   0.2,
			ChronologicalFraction: 0.25,
		})
	if err != nil || len(undated.Chronological) != 0 {
		t.Fatalf("undated chronology = %+v err=%v", undated, err)
	}
}

func TestSplitHeldoutJudgmentsRejectsInvalidInput(t *testing.T) {
	valid := HoldoutSplitConfig{TrainFraction: 0.7, DevelopmentFraction: 0.15}
	cases := []HoldoutSplitConfig{
		{TrainFraction: math.NaN(), DevelopmentFraction: 0.1},
		{TrainFraction: 0, DevelopmentFraction: 0.1},
		{TrainFraction: 0.7, DevelopmentFraction: -0.1},
		{TrainFraction: 0.9, DevelopmentFraction: 0.1},
		{TrainFraction: 0.7, DevelopmentFraction: 0.1, ChronologicalFraction: math.NaN()},
		{TrainFraction: 0.7, DevelopmentFraction: 0.1, ChronologicalFraction: -0.1},
		{TrainFraction: 0.7, DevelopmentFraction: 0.1, ChronologicalFraction: 1},
		{
			TrainFraction: 0.7, DevelopmentFraction: 0.1,
			ChronologicalAfter: time.Now(), ChronologicalFraction: 0.1,
		},
	}
	for _, config := range cases {
		if _, err := SplitHeldoutJudgments(nil, config); err == nil {
			t.Fatalf("config accepted: %+v", config)
		}
	}
	if _, err := SplitHeldoutJudgments([]CanonicalJudgment{{}}, valid); err == nil {
		t.Fatal("empty query cluster accepted")
	}
}
