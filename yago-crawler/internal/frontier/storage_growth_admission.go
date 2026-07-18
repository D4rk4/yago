package frontier

import "context"

type GrowthAdmission interface {
	WaitForGrowth(context.Context) bool
}

func WithGrowthAdmission(admission GrowthAdmission) Option {
	return func(frontier *Frontier) {
		frontier.growthAdmission = admission
	}
}
