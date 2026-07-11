//go:build e2e

package e2e

import "time"

const pollInterval = 250 * time.Millisecond

func waitFor(timeout time.Duration, cond func() bool) bool {
	return waitForEvery(timeout, pollInterval, cond)
}

func waitForEvery(timeout, interval time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(interval)
	}
	return cond()
}
