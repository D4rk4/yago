package learnedrank

import (
	"fmt"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
)

type ModelKind string

const (
	ModelLinearLambdaRank    ModelKind = "linear_lambdarank"
	ModelHistogramLambdaMART ModelKind = "histogram_lambdamart"
)

type Snapshot struct {
	revision  string
	kind      ModelKind
	linear    *rankfit.LinearLambdaRankModel
	histogram *rankfit.HistogramLambdaMARTModel
}

func NewLinearSnapshot(
	revision string,
	model rankfit.LinearLambdaRankModel,
) (Snapshot, error) {
	modelCopy := model
	snapshot := Snapshot{
		revision: revision,
		kind:     ModelLinearLambdaRank,
		linear:   &modelCopy,
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}

	return snapshot, nil
}

func NewHistogramSnapshot(
	revision string,
	model rankfit.HistogramLambdaMARTModel,
) (Snapshot, error) {
	modelCopy := model
	snapshot := Snapshot{
		revision:  revision,
		kind:      ModelHistogramLambdaMART,
		histogram: &modelCopy,
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}

	return snapshot, nil
}

func (s Snapshot) Revision() string {
	return s.revision
}

func (s Snapshot) Kind() ModelKind {
	return s.kind
}

func (s Snapshot) LinearModel() (rankfit.LinearLambdaRankModel, bool) {
	if s.linear == nil || s.kind != ModelLinearLambdaRank {
		return rankfit.LinearLambdaRankModel{}, false
	}

	return *s.linear, true
}

func (s Snapshot) HistogramModel() (rankfit.HistogramLambdaMARTModel, bool) {
	if s.histogram == nil || s.kind != ModelHistogramLambdaMART {
		return rankfit.HistogramLambdaMARTModel{}, false
	}

	return *s.histogram, true
}

func (s Snapshot) Validate() error {
	if !validRevision(s.revision) {
		return fmt.Errorf("model revision must be a bounded ASCII identifier")
	}
	expected := FeatureDefinitions()
	switch s.kind {
	case ModelLinearLambdaRank:
		if s.linear == nil || s.histogram != nil {
			return fmt.Errorf("linear LambdaRank snapshot has invalid model state")
		}
		if err := s.linear.Validate(); err != nil {
			return fmt.Errorf("validate linear LambdaRank snapshot: %w", err)
		}
		if !slices.Equal(s.linear.FeatureDefinitions(), expected) {
			return fmt.Errorf("linear LambdaRank snapshot uses an incompatible feature catalog")
		}
	case ModelHistogramLambdaMART:
		if s.histogram == nil || s.linear != nil {
			return fmt.Errorf("histogram LambdaMART snapshot has invalid model state")
		}
		if err := s.histogram.Validate(); err != nil {
			return fmt.Errorf("validate histogram LambdaMART snapshot: %w", err)
		}
		if !slices.Equal(s.histogram.FeatureDefinitions(), expected) {
			return fmt.Errorf("histogram LambdaMART snapshot uses an incompatible feature catalog")
		}
	default:
		return fmt.Errorf("model kind %q is unsupported", s.kind)
	}

	return nil
}

func validRevision(revision string) bool {
	if len(revision) == 0 || len(revision) > 128 {
		return false
	}
	for index, character := range []byte(revision) {
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		digit := character >= '0' && character <= '9'
		separator := character == '.' || character == '-' || character == '_'
		if index == 0 && !letter && !digit || index > 0 && !letter && !digit && !separator {
			return false
		}
	}

	return true
}
