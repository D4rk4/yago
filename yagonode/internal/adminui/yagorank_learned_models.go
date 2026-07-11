package adminui

import (
	"context"
	"net/http"
)

type LearnedModelKind string

const (
	LearnedModelLinearLambdaRank    LearnedModelKind = "linear_lambdarank"
	LearnedModelHistogramLambdaMART LearnedModelKind = "histogram_lambdamart"
)

type LearnedModelView struct {
	ActiveRevision    string
	ActiveKind        LearnedModelKind
	RollbackAvailable bool
}

type LearnedModelTrainOutcome struct {
	Promoted              bool
	HeldOutNDCGGain       float64
	Confidence            float64
	Reasons               []string
	TrainQueryCount       int
	DevelopmentQueryCount int
	TestQueryCount        int
}

type LearnedRankingSource interface {
	LearnedModel(context.Context) LearnedModelView
	TrainLearnedModel(
		context.Context,
		LearnedModelKind,
	) (LearnedModelTrainOutcome, error)
	RollbackLearnedModel(context.Context) (bool, error)
}

type learnedModelStatusView struct {
	ActiveRevision    string
	ActiveKind        string
	RollbackAvailable bool
}

func (c *Console) applyYagoRankModelTraining(
	w http.ResponseWriter,
	r *http.Request,
	kind LearnedModelKind,
) {
	source, ok := c.ranking.(LearnedRankingSource)
	if !ok {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "Learned model operations are not available.",
		})

		return
	}
	outcome, err := source.TrainLearnedModel(r.Context(), kind)
	if err != nil {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "Model training failed: " + err.Error(),
		})

		return
	}
	outcome.Reasons = append([]string(nil), outcome.Reasons...)
	notice := "Ranking model was not promoted."
	if outcome.Promoted {
		notice = "Ranking model promoted."
	}
	c.renderYagoRank(w, r, yagorankView{
		weights:      c.ranking.Profile(r.Context()).Weights,
		trainOutcome: &outcome,
		notice:       notice,
	})
}

func (c *Console) applyYagoRankModelRollback(w http.ResponseWriter, r *http.Request) {
	source, ok := c.ranking.(LearnedRankingSource)
	if !ok {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "Learned model operations are not available.",
		})

		return
	}
	rolledBack, err := source.RollbackLearnedModel(r.Context())
	if err != nil {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "Model rollback failed: " + err.Error(),
		})

		return
	}
	if !rolledBack {
		c.renderYagoRank(w, r, yagorankView{
			weights: c.ranking.Profile(r.Context()).Weights,
			errMsg:  "No ranking model revision is available for rollback.",
		})

		return
	}
	c.renderYagoRank(w, r, yagorankView{
		weights: c.ranking.Profile(r.Context()).Weights,
		notice:  "Ranking model rolled back.",
	})
}

func learnedModelStatus(
	ctx context.Context,
	ranking RankingSource,
) *learnedModelStatusView {
	source, ok := ranking.(LearnedRankingSource)
	if !ok {
		return nil
	}
	model := source.LearnedModel(ctx)
	view := learnedModelStatusView{
		ActiveRevision:    model.ActiveRevision,
		ActiveKind:        learnedModelKindLabel(model.ActiveKind),
		RollbackAvailable: model.RollbackAvailable,
	}
	if view.ActiveRevision == "" {
		view.ActiveRevision = "None"
	}

	return &view
}

func learnedModelKindLabel(kind LearnedModelKind) string {
	switch kind {
	case LearnedModelLinearLambdaRank:
		return "Linear LambdaRank"
	case LearnedModelHistogramLambdaMART:
		return "Histogram LambdaMART"
	case "":
		return "Built-in"
	default:
		return string(kind)
	}
}
