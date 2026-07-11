package rankingtrain

import "github.com/D4rk4/yago/yagonode/internal/rankfit"

type histogramFeatureFamily struct {
	name     string
	features []int
}

var yagorankHistogramFeatureFamilies = []histogramFeatureFamily{
	{
		name:     "candidate_retrieval",
		features: []int{0, 1, 2, 3, 4, 5, 6},
	},
	{
		name:     "field_dependence",
		features: []int{7, 8, 9, 10, 11, 12, 13, 14, 15},
	},
	{
		name:     "content_quality",
		features: []int{16, 17, 18, 19, 20, 21, 22},
	},
	{
		name:     "temporal_authority",
		features: []int{23, 24, 25, 26, 27},
	},
	{
		name:     "federation_support",
		features: []int{28, 29, 30, 31, 32},
	},
	{
		name:     "relevance_quality",
		features: []int{0, 12, 16, 18, 24, 25, 27},
	},
}

func yagorankHistogramTrainingOptions() rankfit.HistogramLambdaMARTTrainingOptions {
	groups := make([]rankfit.FeatureInteractionGroup, len(yagorankHistogramFeatureFamilies))
	for index, family := range yagorankHistogramFeatureFamilies {
		groups[index] = rankfit.FeatureInteractionGroup{
			Name: family.name, FeatureIndices: append([]int(nil), family.features...),
		}
	}
	options := rankfit.DefaultHistogramLambdaMARTTrainingOptions()
	options.FeatureInteractionGroups = groups

	return options
}
