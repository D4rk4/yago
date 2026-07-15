package searchindex

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRankingWeightCatalogMatchesJSONProfile(t *testing.T) {
	raw, err := json.Marshal(DefaultRankingWeights())
	if err != nil {
		t.Fatalf("marshal defaults: %v", err)
	}
	values := map[string]float64{}
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}
	keys := make(map[string]bool, len(values))
	for _, definition := range RankingWeightDefinitions() {
		if keys[definition.Key] {
			t.Fatalf("duplicate ranking weight definition %q", definition.Key)
		}
		keys[definition.Key] = true
		value, ok := DefaultRankingWeights().Value(definition.Key)
		if !ok || value != definition.Default || values[definition.Key] != value {
			t.Fatalf("definition %q does not match defaults", definition.Key)
		}
	}
	if len(keys) != len(values) {
		t.Fatalf("catalog keys = %v, profile keys = %v", keys, values)
	}
}

func TestRankingWeightDefinitionsReturnsIndependentCopy(t *testing.T) {
	first := RankingWeightDefinitions()
	second := RankingWeightDefinitions()
	first[0].Key = "changed"
	if reflect.DeepEqual(first, second) {
		t.Fatal("ranking definitions share mutable storage")
	}
}

func TestRankingWeightsSetValueAndBounds(t *testing.T) {
	weights := DefaultRankingWeights()
	for _, definition := range RankingWeightDefinitions() {
		value := definition.Maximum
		if !weights.Set(definition.Key, value) {
			t.Fatalf("set rejected %q", definition.Key)
		}
		if got, ok := weights.Value(definition.Key); !ok || got != value {
			t.Fatalf("value %q = %v, %v", definition.Key, got, ok)
		}
	}
	if err := weights.Validate(); err != nil {
		t.Fatalf("maximum profile invalid: %v", err)
	}
	if weights.Set("unknown", 1) {
		t.Fatal("unknown key was accepted")
	}
	if _, ok := weights.Value("unknown"); ok {
		t.Fatal("unknown key was returned")
	}
	weights.URLPrior = 1.01
	if err := weights.Validate(); err == nil {
		t.Fatal("out-of-range prior was accepted")
	}
}

func TestRankingWeightsPersistedValidationKeepsLegacyUpperValues(t *testing.T) {
	weights := DefaultRankingWeights()
	weights.Title = 65
	weights.HostRank = 2
	if err := weights.ValidatePersisted(); err != nil {
		t.Fatalf("legacy upper values: %v", err)
	}
	weights.HostRank = -1
	if err := weights.ValidatePersisted(); err == nil {
		t.Fatal("negative persisted weight was accepted")
	}
}
