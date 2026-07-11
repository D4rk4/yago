package learnedrank

import (
	"math"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestFeatureCatalogCoversRankingEvidenceInSignalOrder(t *testing.T) {
	definitions := FeatureDefinitions()
	if len(definitions) != len(rankingFeatures) {
		t.Fatalf("feature definitions = %d", len(definitions))
	}
	for index, definition := range definitions {
		signal := searchcore.RankingSignal(index)
		if rankingFeatures[index].signal != signal || definition.Name != signal.Name() {
			t.Fatalf("feature %d = %#v", index, definition)
		}
	}
	if (searchcore.SignalSourceCount + 1).Name() != "" {
		t.Fatalf("feature catalog omits signal %d", len(definitions))
	}
	if definitions[2].Direction != rankfit.FeatureDecreasing ||
		definitions[18].Direction != rankfit.FeatureDecreasing ||
		definitions[17].Direction != rankfit.FeatureUnconstrained ||
		definitions[0].Direction != rankfit.FeatureIncreasing {
		t.Fatalf("feature directions = %#v", definitions)
	}
	definitions[0].Name = "changed"
	if FeatureDefinitions()[0].Name != searchcore.SignalRetrievalScore.Name() {
		t.Fatalf("feature definitions were mutable")
	}
}

func TestMapRankingEvidencePreservesKnownValuesAndUnknownZeros(t *testing.T) {
	evidence := searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalRetrievalScore, Value: 4.5},
		searchcore.RankingSignalValue{Signal: searchcore.SignalQualityKnown, Value: 0},
		searchcore.RankingSignalValue{Signal: searchcore.SignalSpamRisk, Value: 0.2},
	)
	vector, known, err := MapRankingEvidence(evidence)
	if err != nil {
		t.Fatalf("MapRankingEvidence: %v", err)
	}
	values := vector.Values()
	if !known || values[0] != 4.5 || values[17] != 0 || values[18] != 0.2 {
		t.Fatalf("mapped values = %v, known = %v", values, known)
	}
	if values[1] != 0 || len(values) != len(rankingFeatures) {
		t.Fatalf("unknown mapping = %v", values)
	}

	empty, known, err := MapRankingEvidence(searchcore.RankingEvidence{})
	if err != nil || known || !reflect.DeepEqual(
		empty.Values(),
		make([]float64, len(rankingFeatures)),
	) {
		t.Fatalf("empty mapping = %v, %v, %v", empty.Values(), known, err)
	}
}

func TestMapRankingEvidenceRejectsOutOfBoundsAndCatalogDrift(t *testing.T) {
	evidence := searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{
			Signal: searchcore.SignalRetrievalScore,
			Value:  math.MaxFloat64,
		},
	)
	if _, _, err := MapRankingEvidence(evidence); err == nil {
		t.Fatalf("out-of-bounds evidence was accepted")
	}

	last := len(rankingFeatures) - 1
	original := rankingFeatures[last]
	rankingFeatures[last] = rankingFeatures[0]
	defer func() { rankingFeatures[last] = original }()
	evidence = searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalSourceCount, Value: 1},
	)
	if _, _, err := MapRankingEvidence(evidence); err == nil {
		t.Fatalf("feature catalog drift was accepted")
	}
}

func TestRankingFeatureIndex(t *testing.T) {
	index, found := rankingFeatureIndex(searchcore.SignalAuthority.Name())
	if !found || rankingFeatures[index].signal != searchcore.SignalAuthority {
		t.Fatalf("authority index = %d, %v", index, found)
	}
	if _, found := rankingFeatureIndex("unknown"); found {
		t.Fatalf("unknown feature was found")
	}
}
