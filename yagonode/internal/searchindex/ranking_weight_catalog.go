package searchindex

const (
	rankingFieldBoostMaximum = 64.0
	rankingPriorMaximum      = 1.0
)

type RankingWeightDefinition struct {
	Key                 string
	Label               string
	Group               string
	Default             float64
	Minimum             float64
	Maximum             float64
	FieldBoost          bool
	BackfillWhenMissing bool
}

var rankingWeightDefinitions = []RankingWeightDefinition{
	{
		Key:        "title",
		Label:      "Title",
		Group:      "Field boosts",
		Default:    6,
		Maximum:    rankingFieldBoostMaximum,
		FieldBoost: true,
	},
	{
		Key:        "anchors",
		Label:      "Anchor text",
		Group:      "Field boosts",
		Default:    4,
		Maximum:    rankingFieldBoostMaximum,
		FieldBoost: true,
	},
	{
		Key:        "headings",
		Label:      "Headings",
		Group:      "Field boosts",
		Default:    3,
		Maximum:    rankingFieldBoostMaximum,
		FieldBoost: true,
	},
	{
		Key:        "url",
		Label:      "URL text",
		Group:      "Field boosts",
		Default:    2,
		Maximum:    rankingFieldBoostMaximum,
		FieldBoost: true,
	},
	{
		Key:        "body",
		Label:      "Body",
		Group:      "Field boosts",
		Default:    1,
		Maximum:    rankingFieldBoostMaximum,
		FieldBoost: true,
	},
	{
		Key:     "hostRank",
		Label:   "Host authority",
		Group:   "Bounded priors",
		Default: 0.3,
		Maximum: rankingPriorMaximum,
	},
	{
		Key:     "freshness",
		Label:   "Freshness",
		Group:   "Bounded priors",
		Default: 0.2,
		Maximum: rankingPriorMaximum,
	},
	{
		Key:     "quality",
		Label:   "Content quality",
		Group:   "Bounded priors",
		Default: 0.2,
		Maximum: rankingPriorMaximum,
	},
	{
		Key:                 "urlPrior",
		Label:               "Short URL prior multiplier",
		Group:               "Bounded priors",
		Default:             1,
		Maximum:             rankingPriorMaximum,
		BackfillWhenMissing: true,
	},
	{
		Key:                 "orderedProximity",
		Label:               "Ordered proximity",
		Group:               "Term dependence",
		Default:             0.12,
		Maximum:             rankingPriorMaximum,
		BackfillWhenMissing: true,
	},
	{
		Key:     "proximity",
		Label:   "Unordered proximity",
		Group:   "Term dependence",
		Default: 0.15,
		Maximum: rankingPriorMaximum,
	},
	{
		Key:                 "lexicalBlend",
		Label:               "Lexical evidence blend",
		Group:               "Lexical reranking",
		Default:             0.25,
		Maximum:             rankingPriorMaximum,
		BackfillWhenMissing: true,
	},
	{
		Key:                 "lexicalGapAgreement",
		Label:               "Original-gap agreement",
		Group:               "Lexical reranking",
		Default:             0.05,
		Maximum:             rankingPriorMaximum,
		BackfillWhenMissing: true,
	},
}

func RankingWeightDefinitions() []RankingWeightDefinition {
	return append([]RankingWeightDefinition(nil), rankingWeightDefinitions...)
}

func (weights RankingWeights) Value(key string) (float64, bool) {
	switch key {
	case "title":
		return weights.Title, true
	case "headings":
		return weights.Headings, true
	case "anchors":
		return weights.Anchors, true
	case "body":
		return weights.Body, true
	case "url":
		return weights.URL, true
	case "hostRank":
		return weights.HostRank, true
	case "freshness":
		return weights.Freshness, true
	case "quality":
		return weights.Quality, true
	case "urlPrior":
		return weights.URLPrior, true
	case "orderedProximity":
		return weights.OrderedProximity, true
	case "proximity":
		return weights.Proximity, true
	case "lexicalBlend":
		return weights.LexicalBlend, true
	case "lexicalGapAgreement":
		return weights.LexicalGapAgreement, true
	default:
		return 0, false
	}
}

func (weights *RankingWeights) Set(key string, value float64) bool {
	switch key {
	case "title":
		weights.Title = value
	case "headings":
		weights.Headings = value
	case "anchors":
		weights.Anchors = value
	case "body":
		weights.Body = value
	case "url":
		weights.URL = value
	case "hostRank":
		weights.HostRank = value
	case "freshness":
		weights.Freshness = value
	case "quality":
		weights.Quality = value
	case "urlPrior":
		weights.URLPrior = value
	case "orderedProximity":
		weights.OrderedProximity = value
	case "proximity":
		weights.Proximity = value
	case "lexicalBlend":
		weights.LexicalBlend = value
	case "lexicalGapAgreement":
		weights.LexicalGapAgreement = value
	default:
		return false
	}

	return true
}
