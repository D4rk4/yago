package frontiercheckpoint

import "github.com/D4rk4/yago/yago-crawler/internal/crawlpace"

func validateHostPaceState(state crawlpace.HostState) error {
	if state.Generation == 0 || state.BackoffPenalty < 0 ||
		(state.BackoffFailures > 0 && state.BackoffPenalty == 0) ||
		(state.BackoffPenalty > 0 && state.BackoffUntil.IsZero()) {
		return ErrInvalidHostState
	}

	return nil
}

func validateHostPaceProgress(progress HostProgress) error {
	if progress.PaceCapacity < 0 ||
		(progress.PaceCapacity == 0 && progress.Pace != (crawlpace.HostState{})) {
		return ErrInvalidHostState
	}
	if progress.PaceCapacity > 0 {
		return validateHostPaceState(progress.Pace)
	}

	return nil
}
