package pipeline

import "context"

type FetchStartAdmission interface {
	Wait(context.Context) error
}

func WithFetchStartAdmission(admission FetchStartAdmission) Option {
	return func(pipeline *Pipeline) {
		pipeline.fetchStartAdmission = admission
	}
}
