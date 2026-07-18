package adminui

import (
	"context"
	"net/url"
)

type SearchExplanationSource interface {
	Explain(ctx context.Context, query string, global bool) (SearchExplanation, error)
}

type SearchExplanation struct {
	Query           string
	Global          bool
	ModelRevision   string
	ModelKind       string
	PartialFailures []string
	Results         []SearchExplanationResult
}

type SearchExplanationResult struct {
	FinalRank           int
	URL                 string
	Source              string
	Score               float64
	RetrievalScore      float64
	Quality             float64
	QualityKnown        bool
	SpamRisk            float64
	SpamRiskKnown       bool
	FunctionWordShare   float64
	FunctionWordKnown   bool
	SymbolShare         float64
	SymbolKnown         bool
	AlphabeticShare     float64
	AlphabeticKnown     bool
	UniqueTokenShare    float64
	UniqueTokenKnown    bool
	Proximity           float64
	ProximityKnown      bool
	FieldContributions  []SearchFieldContribution
	Evidence            []SearchRankingSignal
	Fusion              []SearchFusionContribution
	RetrievalDiagnostic string
	Learned             *SearchLearnedExplanation
}

type SearchFusionContribution struct {
	Branch       string
	Rank         int
	Contribution float64
}

type SearchRankingSignal struct {
	Name  string
	Value float64
}

type SearchFieldContribution struct {
	Name  string
	Score float64
}

type SearchLearnedExplanation struct {
	OriginalRank  int
	ModelRank     int
	FinalRank     int
	OriginalScore float64
	Score         float64
	Signals       []SearchLearnedSignal
	Trees         []SearchLearnedTree
}

type SearchLearnedSignal struct {
	Name            string
	Known           bool
	Value           float64
	Used            bool
	NormalizedValue float64
	Weight          float64
	Contribution    float64
}

type SearchLearnedTree struct {
	Index            int
	InteractionGroup string
	Contribution     float64
	Decisions        []SearchLearnedTreeDecision
}

type SearchLearnedTreeDecision struct {
	Name              string
	Known             bool
	TerminatedMissing bool
	NormalizedValue   float64
	Threshold         float64
	WentLeft          bool
}

func searchExplanationURL(query string, global bool) string {
	values := url.Values{}
	values.Set("q", query)
	if global {
		values.Set("scope", "global")
	} else {
		values.Set("scope", "local")
	}

	return (&url.URL{Path: yagorankPath, RawQuery: values.Encode()}).String()
}
