package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/rankingtrain"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// ranker fits the ranking weights to the judgment set; the concrete tuner
// satisfies it, and a fake stands in for it under test.
type ranker interface {
	Tune(ctx context.Context) (rankfit.Report, error)
}

// rankingConsole adapts the ranking holder, the coordinate-ascent tuner, and the
// judgment store to the admin console's RankingSource.
type rankingConsole struct {
	holder  *rankingprofile.Holder
	tuner   ranker
	curated curatedJudgments
	trainer rankingTrainingRunner
	models  rankingModelCatalog
	trust   trustedDomainCatalog
}

type rankingConsoleLearning struct {
	trainer rankingTrainingRunner
	models  rankingModelCatalog
	trust   trustedDomainCatalog
}

// newRankingConsole wires the console section; a nil holder (the in-memory
// fallback deployment carries no persisted profile) yields a nil source so the
// section renders its unavailable state.
func newRankingConsole(
	holder *rankingprofile.Holder,
	tuner ranker,
	curated curatedJudgments,
	learning ...rankingConsoleLearning,
) adminui.RankingSource {
	if holder == nil {
		return nil
	}
	console := &rankingConsole{holder: holder, tuner: tuner, curated: curated}
	if len(learning) != 0 {
		console.trainer = learning[0].trainer
		console.models = learning[0].models
		console.trust = learning[0].trust
	}

	return console
}

func (rc *rankingConsole) Profile(ctx context.Context) adminui.RankingProfile {
	judgmentCount, judgmentsAvailable := rc.judgmentStatus(ctx)
	profile := adminui.RankingProfile{
		Weights:            weightsView(rc.holder.Current()),
		JudgmentCount:      judgmentCount,
		JudgmentsAvailable: judgmentsAvailable,
	}
	if source, ok := rc.tuner.(rankingTrainingReadinessSource); ok {
		readiness := source.TrainingReadiness(ctx)
		profile.TrainingReadinessAvailable = readiness.Available
		profile.ModelTrainingReady = readiness.Ready
		profile.TrainingJudgmentCount = readiness.Judgments
		profile.TrainingQueryClusterCount = readiness.QueryClusters
		profile.HeldoutQueryClusterCount = readiness.HeldoutQueryClusters
		profile.MinimumHeldoutQueryClusters = readiness.MinimumHeldoutQueryClusters
	}

	return profile
}

func (rc *rankingConsole) judgmentStatus(ctx context.Context) (int, bool) {
	if rc.curated == nil {
		return 0, false
	}
	stored, err := rc.curated.List(ctx)
	if err != nil {
		return 0, false
	}

	return len(stored), true
}

func (rc *rankingConsole) Tune(ctx context.Context) (adminui.RankingTuneResult, error) {
	report, err := rc.tuner.Tune(ctx)
	if err != nil {
		return adminui.RankingTuneResult{}, fmt.Errorf("tune ranking: %w", err)
	}

	return adminui.RankingTuneResult{
		BeforeNDCG: report.BeforeNDCG,
		AfterNDCG:  report.AfterNDCG,
		Rounds:     report.Rounds,
		Improved:   report.Improved(),
		Proposed:   weightsView(report.After),
	}, nil
}

func (rc *rankingConsole) Apply(ctx context.Context, values map[string]float64) error {
	weights := weightsFromMap(rc.holder.Current(), values)
	if err := weights.Validate(); err != nil {
		return fmt.Errorf("validate weights: %w", err)
	}
	if err := rc.holder.Set(ctx, weights); err != nil {
		return fmt.Errorf("save ranking profile: %w", err)
	}

	return nil
}

func (rc *rankingConsole) LearnedModel(context.Context) adminui.LearnedModelView {
	if rc.models == nil {
		return adminui.LearnedModelView{}
	}
	status := rc.models.Snapshot().Status

	return adminui.LearnedModelView{
		ActiveRevision:    status.Current.Revision,
		ActiveKind:        adminui.LearnedModelKind(status.Current.Kind),
		RollbackAvailable: len(status.Rollback) != 0,
	}
}

func (rc *rankingConsole) TrainLearnedModel(
	ctx context.Context,
	kind adminui.LearnedModelKind,
) (adminui.LearnedModelTrainOutcome, error) {
	if rc.trainer == nil {
		return adminui.LearnedModelTrainOutcome{}, fmt.Errorf("ranking model trainer unavailable")
	}
	if source, ok := rc.tuner.(rankingTrainingReadinessSource); ok {
		readiness := source.TrainingReadiness(ctx)
		if !readiness.Ready {
			return adminui.LearnedModelTrainOutcome{}, fmt.Errorf(
				"ranking evidence is not ready for held-out model promotion",
			)
		}
	}
	family := rankingtrain.ModelFamily(kind)
	outcome, err := rc.trainer.Train(ctx, "", family)
	if err != nil {
		return adminui.LearnedModelTrainOutcome{}, fmt.Errorf(
			"train learned ranking model: %w",
			err,
		)
	}

	return adminui.LearnedModelTrainOutcome{
		Promoted:              outcome.Promoted,
		HeldOutNDCGGain:       outcome.Decision.Confidence.ObservedRelativeGain,
		Confidence:            outcome.Decision.Confidence.Confidence,
		Reasons:               append([]string(nil), outcome.Decision.Reasons...),
		TrainQueryCount:       outcome.Counts.Train.Queries,
		DevelopmentQueryCount: outcome.Counts.Development.Queries,
		TestQueryCount:        outcome.Counts.Test.Queries,
	}, nil
}

func (rc *rankingConsole) RollbackLearnedModel(ctx context.Context) (bool, error) {
	if rc.models == nil {
		return false, fmt.Errorf("ranking model catalog unavailable")
	}

	rolledBack, err := rc.models.Rollback(ctx)
	if err != nil {
		return false, fmt.Errorf("rollback learned ranking model: %w", err)
	}

	return rolledBack, nil
}

// weightsView renders the live weights in the console's display order, reading
// each value from the profile by its JSON key.
func weightsView(weights searchindex.RankingWeights) []adminui.RankingWeight {
	definitions := searchindex.RankingWeightDefinitions()
	view := make([]adminui.RankingWeight, 0, len(definitions))
	for _, definition := range definitions {
		value, _ := weights.Value(definition.Key)
		view = append(view, adminui.RankingWeight{
			Key:        definition.Key,
			Label:      definition.Label,
			Group:      definition.Group,
			Value:      value,
			Default:    definition.Default,
			Minimum:    definition.Minimum,
			Maximum:    definition.Maximum,
			OutOfRange: value < definition.Minimum || value > definition.Maximum,
		})
	}

	return view
}

// weightsFromMap overlays the supplied values onto the base weights by JSON key,
// so keys the form omits keep their current value.
func weightsFromMap(
	base searchindex.RankingWeights,
	overlay map[string]float64,
) searchindex.RankingWeights {
	for key, value := range overlay {
		base.Set(key, value)
	}

	return base
}
