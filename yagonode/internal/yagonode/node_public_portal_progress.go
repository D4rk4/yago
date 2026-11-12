package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func withPortalProgressiveSearch(ctx context.Context) context.Context {
	if searchcore.SearchProgressObserverFrom(ctx) != nil {
		return ctx
	}

	return searchcore.WithSearchProgressObserver(ctx, discardPortalSearchProgress)
}

func discardPortalSearchProgress(searchcore.SearchProgress) {}
