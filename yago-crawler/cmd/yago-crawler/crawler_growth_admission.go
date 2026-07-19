package main

import (
	"context"
	"fmt"
)

type crawlerGrowthWaiter interface {
	WaitForGrowth(context.Context) bool
}

type crawlerStateGrowthWaiter interface {
	WaitForGrowth(context.Context) (bool, error)
}

type crawlerGrowthChecker interface {
	CheckGrowth() error
}

type crawlerNewGrowthAdmission struct {
	storage       crawlerGrowthWaiter
	frontierState crawlerStateGrowthWaiter
}

func newCrawlerNewGrowthAdmission(
	storage crawlerGrowthWaiter,
	frontierState crawlerStateGrowthWaiter,
) *crawlerNewGrowthAdmission {
	return &crawlerNewGrowthAdmission{storage: storage, frontierState: frontierState}
}

func (admission *crawlerNewGrowthAdmission) WaitForGrowth(
	ctx context.Context,
) (bool, error) {
	for {
		if admission.storage != nil && !admission.storage.WaitForGrowth(ctx) {
			return false, nil
		}
		if admission.frontierState != nil {
			allowed, err := admission.frontierState.WaitForGrowth(ctx)
			if err != nil {
				return allowed, fmt.Errorf("wait for crawler frontier-state growth: %w", err)
			}
			if !allowed {
				return false, nil
			}
		}
		storage, ok := admission.storage.(crawlerGrowthChecker)
		if !ok || storage.CheckGrowth() == nil {
			return true, nil
		}
	}
}
