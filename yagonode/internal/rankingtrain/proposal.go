package rankingtrain

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func BuildProposal(
	ctx context.Context,
	searcher searchcore.Searcher,
	judgments []searcheval.Judgment,
	config Config,
) (Proposal, error) {
	config, err := normalizedConfig(config)
	if err != nil {
		return Proposal{}, err
	}
	if ctx == nil {
		return Proposal{}, fmt.Errorf("training context must not be nil")
	}
	if searcher == nil {
		return Proposal{}, fmt.Errorf("candidate searcher must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Proposal{}, fmt.Errorf("build ranking proposal: %w", err)
	}
	datasets, err := retrieveCandidateDatasets(ctx, searcher, judgments)
	if err != nil {
		return Proposal{}, err
	}
	split, err := splitCandidateDatasets(datasets, config.Split)
	if err != nil {
		return Proposal{}, err
	}
	model, snapshot, training, err := trainRankingModel(
		ctx,
		config.Family,
		config.Revision,
		split.train,
	)
	if err != nil {
		return Proposal{}, err
	}
	incumbent := incumbentRankingModel(config.Incumbent)
	development, test, err := evaluateProposalSplits(ctx, model, incumbent, split, config)
	if err != nil {
		return Proposal{}, err
	}
	if err := ctx.Err(); err != nil {
		return Proposal{}, fmt.Errorf("build ranking proposal: %w", err)
	}
	decision, err := proposalPromotionDecision(test, config.PromotionPolicy)
	if err != nil {
		return Proposal{}, fmt.Errorf("decide held-out promotion: %w", err)
	}

	return Proposal{
		snapshot:    snapshot,
		counts:      datasetCounts(split),
		training:    training,
		development: cloneEvaluationComparison(development),
		test:        cloneEvaluationComparison(test),
		decision:    clonePromotionDecision(decision),
	}, nil
}

func incumbentRankingModel(snapshot *learnedrank.Snapshot) *trainedRankingModel {
	if snapshot == nil {
		return nil
	}
	model, _ := trainedRankingModelForSnapshot(*snapshot)

	return &model
}

func evaluateProposalSplits(
	ctx context.Context,
	model trainedRankingModel,
	incumbent *trainedRankingModel,
	split datasetSplit,
	config Config,
) (EvaluationComparison, EvaluationComparison, error) {
	development, err := evaluateRankingModel(
		ctx,
		model,
		incumbent,
		split.development,
		config.MeasurementClock,
	)
	if err != nil {
		return EvaluationComparison{}, EvaluationComparison{}, err
	}
	test, err := evaluateRankingModel(
		ctx,
		model,
		incumbent,
		split.test,
		config.MeasurementClock,
	)
	if err != nil {
		return EvaluationComparison{}, EvaluationComparison{}, err
	}

	return development, test, nil
}

func proposalPromotionDecision(
	test EvaluationComparison,
	policy searcheval.PromotionPolicy,
) (searcheval.PromotionDecision, error) {
	var decision searcheval.PromotionDecision
	var err error
	if test.Incumbent == nil {
		decision, err = searcheval.DecideHeldoutPromotion(
			test.Baseline,
			test.Candidate,
			policy,
		)
	} else {
		decision, err = searcheval.DecideHeldoutPromotionWithIncumbent(
			test.Baseline,
			*test.Incumbent,
			test.Candidate,
			policy,
		)
	}
	if err != nil {
		return searcheval.PromotionDecision{}, fmt.Errorf("compare held-out ranking: %w", err)
	}

	return decision, nil
}

func normalizedConfig(config Config) (Config, error) {
	if config.Family == "" {
		config.Family = FamilyLinearLambdaRank
	}
	if config.Family != FamilyLinearLambdaRank && config.Family != FamilyHistogramLambdaMART {
		return Config{}, fmt.Errorf("model family %q is unsupported", config.Family)
	}
	if config.Split == (searcheval.HoldoutSplitConfig{}) {
		config.Split = searcheval.DefaultHoldoutSplitConfig()
	}
	if config.MeasurementClock == nil {
		config.MeasurementClock = time.Now
	}
	if config.Incumbent != nil {
		if err := config.Incumbent.Validate(); err != nil {
			return Config{}, fmt.Errorf("active incumbent: %w", err)
		}
	}
	if err := validateModelRevision(config.Revision, rankfit.NewLinearLambdaRankModel); err != nil {
		return Config{}, err
	}

	return config, nil
}

type linearModelFactory func(
	[]rankfit.FeatureDefinition,
	[]float64,
) (rankfit.LinearLambdaRankModel, error)

func validateModelRevision(revision string, factory linearModelFactory) error {
	definitions := learnedrank.FeatureDefinitions()
	model, err := factory(definitions, make([]float64, len(definitions)))
	if err != nil {
		return fmt.Errorf("build revision validation model: %w", err)
	}
	if _, err := learnedrank.NewLinearSnapshot(revision, model); err != nil {
		return fmt.Errorf("model revision: %w", err)
	}

	return nil
}

func clonePromotionDecision(
	decision searcheval.PromotionDecision,
) searcheval.PromotionDecision {
	decision.Reasons = append([]string(nil), decision.Reasons...)
	if decision.IncumbentConfidence != nil {
		confidence := *decision.IncumbentConfidence
		decision.IncumbentConfidence = &confidence
	}

	return decision
}
