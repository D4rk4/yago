package searchindex

import "testing"

func TestCacheIdentityIncludesEveryRankingWeight(t *testing.T) {
	base := SearchRequest{Query: "query", Weights: DefaultRankingWeights()}
	baseKey := cacheKey(base)
	for _, definition := range RankingWeightDefinitions() {
		request := base
		request.Weights = base.Weights
		value, _ := request.Weights.Value(definition.Key)
		changed := definition.Minimum
		if changed == value {
			changed = definition.Maximum
		}
		request.Weights.Set(definition.Key, changed)
		if cacheKey(request) == baseKey {
			t.Fatalf("cache identity ignored %q", definition.Key)
		}
	}
}
