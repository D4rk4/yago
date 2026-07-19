package frontier

import (
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type StateGrowthAdmission interface {
	CheckGrowth() error
}

func (f *Frontier) stateGrowthAllowed() (bool, error) {
	if f.stateGrowthAdmission == nil {
		return true, nil
	}
	err := f.stateGrowthAdmission.CheckGrowth()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, frontiercheckpoint.ErrStateMaximum) {
		return false, nil
	}

	return false, fmt.Errorf("check frontier state growth: %w", err)
}

func WithStateGrowthAdmission(admission StateGrowthAdmission) Option {
	return func(frontier *Frontier) {
		frontier.stateGrowthAdmission = admission
	}
}
