package learnedrank

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

type SignalExplanation struct {
	Signal          searchcore.RankingSignal `json:"signal"`
	Name            string                   `json:"name"`
	Known           bool                     `json:"known"`
	Value           float64                  `json:"value"`
	Used            bool                     `json:"used"`
	NormalizedValue float64                  `json:"normalized_value"`
	Weight          float64                  `json:"weight"`
	Contribution    float64                  `json:"contribution"`
}

type TreeDecision struct {
	Signal            searchcore.RankingSignal `json:"signal"`
	Name              string                   `json:"name"`
	Known             bool                     `json:"known"`
	TerminatedMissing bool                     `json:"terminated_missing"`
	NormalizedValue   float64                  `json:"normalized_value"`
	Threshold         float64                  `json:"threshold"`
	WentLeft          bool                     `json:"went_left"`
}

type TreeExplanation struct {
	TreeIndex        int            `json:"tree_index"`
	InteractionGroup string         `json:"interaction_group"`
	Contribution     float64        `json:"contribution"`
	Decisions        []TreeDecision `json:"decisions"`
}

type ResultExplanation struct {
	Identity           string              `json:"identity"`
	OriginalRank       int                 `json:"original_rank"`
	ModelRank          int                 `json:"model_rank"`
	FinalRank          int                 `json:"final_rank"`
	OriginalScore      float64             `json:"original_score"`
	Score              float64             `json:"score"`
	Signals            []SignalExplanation `json:"signals"`
	Trees              []TreeExplanation   `json:"trees,omitempty"`
	documentIdentifier string
}

type Outcome struct {
	Results          []searchcore.Result
	Applied          bool
	SnapshotRevision string
	ModelKind        ModelKind
	Explanations     []ResultExplanation
}
