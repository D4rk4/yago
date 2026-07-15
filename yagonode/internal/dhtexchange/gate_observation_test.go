package dhtexchange

import "testing"

func TestEvaluateGatesFailsClosedForUnknownOperationalState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		forget func(*GateState)
		gate   GateName
		reason string
	}{
		{
			"local rwi",
			func(state *GateState) { state.LocalRWIKnown = false },
			GateLocalRWI,
			GateLocalRWIUnavailableReason,
		},
		{
			"crawl queue",
			func(state *GateState) { state.CrawlQueueKnown = false },
			GateCrawlIdle,
			GateCrawlQueueUnavailableReason,
		},
		{
			"index queue",
			func(state *GateState) { state.IndexQueueKnown = false },
			GateIndexIdle,
			GateIndexQueueUnavailableReason,
		},
		{
			"storage",
			func(state *GateState) { state.StorageKnown = false },
			GateStorageAvailable,
			GateStorageStatusUnavailableReason,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := openGateState()
			test.forget(&state)
			config := DefaultGateConfig()
			config.AllowWhileIndexing = false

			report := EvaluateGates(state, config)
			assertClosed(t, report, test.gate, test.reason)
		})
	}
}

func TestEvaluateGatesHonorsQueueOverridesWhenStateIsUnknown(t *testing.T) {
	t.Parallel()

	state := openGateState()
	state.CrawlQueueKnown = false
	state.IndexQueueKnown = false
	config := DefaultGateConfig()
	config.AllowWhileCrawling = true
	config.AllowWhileIndexing = true

	report := EvaluateGates(state, config)
	if !report.Open {
		t.Fatalf("Open = false, reason %q", report.BlockingReason)
	}
	assertOpen(t, report, GateCrawlIdle)
	assertOpen(t, report, GateIndexIdle)
}
