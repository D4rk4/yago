package rankfit

import (
	"context"
	"fmt"
)

type trainingWork struct {
	examples        int
	preferencePairs int
}

func validateTrainingWork(
	ctx context.Context,
	queryGroups []QueryGroup,
	dimension int,
) error {
	work := trainingWork{}
	for _, group := range queryGroups {
		if err := validateTrainingGroupWork(ctx, group, dimension, &work); err != nil {
			return err
		}
	}

	return nil
}

func validateTrainingGroupWork(
	ctx context.Context,
	group QueryGroup,
	dimension int,
	work *trainingWork,
) error {
	if err := trainingWorkContextError(ctx); err != nil {
		return err
	}
	if err := group.validate(); err != nil {
		return err
	}
	if group.examples[0].features.Dimension() != dimension {
		return dimensionMismatchError(dimension, group.examples[0].features.Dimension())
	}
	if err := reserveTrainingExamples(work, len(group.examples), dimension); err != nil {
		return err
	}

	return countTrainingGroupPreferences(ctx, group.examples, work)
}

func reserveTrainingExamples(work *trainingWork, examples, dimension int) error {
	if examples > maximumTrainingExamples-work.examples {
		return fmt.Errorf(
			"training examples exceed the limit of %d",
			maximumTrainingExamples,
		)
	}
	work.examples += examples
	if dimension > 0 && work.examples > maximumTrainingFeatureValues/dimension {
		return fmt.Errorf(
			"training feature values exceed the limit of %d",
			maximumTrainingFeatureValues,
		)
	}

	return nil
}

func countTrainingGroupPreferences(
	ctx context.Context,
	examples []RankingExample,
	work *trainingWork,
) error {
	for left := range examples {
		if err := trainingWorkContextError(ctx); err != nil {
			return err
		}
		for right := left + 1; right < len(examples); right++ {
			if err := periodicTrainingWorkContextError(ctx, right); err != nil {
				return err
			}
			if examples[left].relevance == examples[right].relevance {
				continue
			}
			if work.preferencePairs == maximumTrainingPreferencePairs {
				return fmt.Errorf(
					"training preference pairs exceed the limit of %d",
					maximumTrainingPreferencePairs,
				)
			}
			work.preferencePairs++
		}
	}

	return nil
}

func periodicTrainingWorkContextError(ctx context.Context, position int) error {
	if position&63 != 0 {
		return nil
	}

	return trainingWorkContextError(ctx)
}

func trainingWorkContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validate ranking training work: %w", err)
	}

	return nil
}
