package fleetfetchstart

import (
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func measuredPermitDeliveryAllowance(startedAt time.Time, receivedAt time.Time) time.Duration {
	return min(
		max(receivedAt.Sub(startedAt), 0),
		yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance,
	)
}

func nextLocalPermitOpening(
	scheduledOpening time.Time,
	previousUse time.Time,
	interval time.Duration,
) time.Time {
	if previousUse.IsZero() || !previousUse.Add(interval).After(scheduledOpening) {
		return scheduledOpening
	}

	return previousUse.Add(interval)
}
