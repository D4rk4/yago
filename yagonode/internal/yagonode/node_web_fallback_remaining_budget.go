package yagonode

import (
	"context"
	"time"
)

const webFallbackAssemblyReserve = 100 * time.Millisecond

func webFallbackSequentialReserve(config webFallbackConfig) time.Duration {
	if effectiveWebFallbackPrivacy(config) == webFallbackPrivacyAlways {
		return 0
	}

	return max(recoverySearchBudget, localExactRecoveryBudget) +
		webFallbackProviderBudget + webFallbackAssemblyReserve
}

func remainingExactStageBudget(ctx context.Context, ceiling, reserve time.Duration) time.Duration {
	deadline, bounded := ctx.Deadline()
	if !bounded || reserve <= 0 {
		return ceiling
	}
	available := time.Until(deadline) - reserve
	if available <= 0 {
		return ceiling
	}

	return min(ceiling, available)
}
