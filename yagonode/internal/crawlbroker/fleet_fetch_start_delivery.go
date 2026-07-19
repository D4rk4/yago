package crawlbroker

import (
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func validFleetFetchStartPermitDeliveryAllowance(allowance time.Duration) bool {
	return allowance >= 0 &&
		allowance <= yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance
}

func fleetFetchStartPermitWindowWidth(
	interval time.Duration,
	deliveryAllowance time.Duration,
) time.Duration {
	return interval + deliveryAllowance
}

func fleetFetchStartLeaseExpiry(current time.Time, finalPermitClosesAt time.Time) time.Time {
	if finalPermitClosesAt.After(current) {
		return finalPermitClosesAt
	}

	return current
}
