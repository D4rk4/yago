package yagocrawlcontract

import "testing"

func TestHeartbeatLimitsAreIndependentFromFetchConcurrency(t *testing.T) {
	if MaximumHeartbeatActiveLeases != 1024 ||
		MaximumHeartbeatActiveLeases <= MaximumFetchWorkerConcurrency ||
		MaximumHeartbeatActiveLeases < 20 ||
		MaximumHeartbeatDirectiveAcknowledgments != 256 {
		t.Fatalf(
			"heartbeat limits = %d/%d",
			MaximumHeartbeatActiveLeases,
			MaximumHeartbeatDirectiveAcknowledgments,
		)
	}
}
