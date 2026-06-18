//go:build e2e

package e2e

import "time"

const (
	httpPort = "8090/tcp"

	stageNodeReady         = 20 * time.Second
	stageYaCyReady         = 60 * time.Second
	stageHelloHandshake    = 15 * time.Second
	stageSeniorPromotion   = 45 * time.Second
	stagePushIndexed       = 30 * time.Second
	stageDispatcherRestart = 60 * time.Second
	stageTransferReceived  = 45 * time.Second

	pollInterval = 250 * time.Millisecond
)

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(pollInterval)
	}
	return cond()
}
