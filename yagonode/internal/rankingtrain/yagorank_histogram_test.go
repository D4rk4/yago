package rankingtrain

import (
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
)

func TestYagoRankHistogramUsesBoundedFeatureFamilies(t *testing.T) {
	definitions := learnedrank.FeatureDefinitions()
	options := yagorankHistogramTrainingOptions()
	if options.MaximumTrees != 64 || options.MaximumDepth != 4 ||
		len(options.FeatureInteractionGroups) != len(yagorankHistogramFeatureFamilies) {
		t.Fatalf("histogram options = %#v", options)
	}
	for index, group := range options.FeatureInteractionGroups {
		if group.Name != yagorankHistogramFeatureFamilies[index].name ||
			len(group.FeatureIndices) < 2 {
			t.Fatalf("interaction group = %#v", group)
		}
	}
	options.FeatureInteractionGroups[0].FeatureIndices[0] = -1
	second := yagorankHistogramTrainingOptions()
	if second.FeatureInteractionGroups[0].FeatureIndices[0] < 0 {
		t.Fatal("histogram groups share mutable state")
	}
	if len(definitions) != 33 {
		t.Fatalf("feature catalog has %d slots", len(definitions))
	}
	want := []int{0, 12, 16, 18, 24, 25, 27}
	if !reflect.DeepEqual(second.FeatureInteractionGroups[5].FeatureIndices, want) {
		t.Fatalf("cross-family features = %v", second.FeatureInteractionGroups[5].FeatureIndices)
	}
}
