package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func completedOrExpiredInteractiveSearch(
	ctx context.Context,
	hardContext context.Context,
	req searchcore.Request,
	outcomes <-chan interactiveSearchOutcome,
) (searchcore.Response, error) {
	select {
	case outcome := <-outcomes:
		return completedInteractiveSearch(ctx, req, outcome)
	default:
		return interactiveSearchFailure(
			ctx,
			req,
			searchcore.Response{},
			context.Cause(hardContext),
		)
	}
}

func completedInteractiveSearch(
	ctx context.Context,
	req searchcore.Request,
	outcome interactiveSearchOutcome,
) (searchcore.Response, error) {
	if outcome.failure != nil {
		panic(outcome.failure)
	}
	if outcome.err != nil {
		return interactiveSearchFailure(ctx, req, outcome.response, outcome.err)
	}

	return outcome.response, nil
}
