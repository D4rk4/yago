package dhtexchange

import "testing"

func openGateState() GateState {
	return GateState{
		PublicReachable:  true,
		LocalPeerKnown:   true,
		ConnectedPeers:   DefaultMinimumConnectedPeers,
		LocalRWIWords:    DefaultMinimumRWIWords,
		LocalRWIKnown:    true,
		CrawlQueueKnown:  true,
		IndexQueueKnown:  true,
		StorageAvailable: true,
		StorageKnown:     true,
	}
}

func TestEvaluateGatesReportsOpenWhenAllGatesPass(t *testing.T) {
	t.Parallel()

	report := EvaluateGates(openGateState(), DefaultGateConfig())
	if !report.Open {
		t.Fatalf("Open = false, reason %q", report.BlockingReason)
	}
	if report.BlockingReason != "" {
		t.Fatalf("BlockingReason = %q, want empty", report.BlockingReason)
	}
	for _, result := range report.Results {
		if !result.Open || result.Reason != GateOpenReason {
			t.Fatalf("result = %#v, want open", result)
		}
	}
}

func TestEvaluateGatesUsesUpstreamDefaultsForNumericThresholds(t *testing.T) {
	t.Parallel()

	state := openGateState()
	state.ConnectedPeers = DefaultMinimumConnectedPeers - 1
	state.LocalRWIWords = DefaultMinimumRWIWords - 1
	config := DefaultGateConfig()
	config.MinimumConnectedPeer = 0
	config.MinimumRWIWord = 0

	report := EvaluateGates(state, config)
	assertClosed(t, report, GateNetworkSize, GateNetworkTooSmallReason)
	assertClosed(t, report, GateLocalRWI, GateLocalRWITooSmallReason)
}

func TestEvaluateGatesReportsEveryClosedGateReason(t *testing.T) {
	t.Parallel()

	config := DefaultGateConfig()
	config.NetworkDHTEnabled = false
	config.DistributionEnabled = false
	config.AllowWhileCrawling = false
	config.AllowWhileIndexing = false
	state := GateState{
		OnlineCaution:    "proxy",
		PublicReachable:  false,
		LocalPeerKnown:   false,
		LocalPeerVirgin:  true,
		ConnectedPeers:   DefaultMinimumConnectedPeers - 1,
		LocalRWIWords:    DefaultMinimumRWIWords - 1,
		LocalRWIKnown:    true,
		CrawlQueueSize:   1,
		CrawlQueueKnown:  true,
		IndexQueueSize:   2,
		IndexQueueKnown:  true,
		StorageAvailable: false,
		StorageKnown:     true,
	}

	report := EvaluateGates(state, config)
	if report.Open {
		t.Fatal("Open = true, want closed")
	}
	if report.BlockingReason != GateOnlineCautionReason {
		t.Fatalf("BlockingReason = %q, want %q", report.BlockingReason, GateOnlineCautionReason)
	}
	assertClosed(t, report, GateOnlineCaution, GateOnlineCautionReason)
	assertClosed(t, report, GatePublicReachability, GatePublicReachabilityReason)
	assertClosed(t, report, GateLocalPeer, GateLocalPeerMissingReason)
	assertClosed(t, report, GateLocalPeerMature, GateLocalPeerVirginReason)
	assertClosed(t, report, GateNetworkSize, GateNetworkTooSmallReason)
	assertClosed(t, report, GateNetworkDHT, GateNetworkDHTDisabledReason)
	assertClosed(t, report, GateDistributionEnabled, GateDistributionDisabledReason)
	assertClosed(t, report, GateLocalRWI, GateLocalRWITooSmallReason)
	assertClosed(t, report, GateCrawlIdle, GateCrawlActiveReason)
	assertClosed(t, report, GateIndexIdle, GateIndexActiveReason)
	assertClosed(t, report, GateStorageAvailable, GateStorageUnavailableReason)
}

func TestEvaluateGatesHonorsCrawlAndIndexingOverrides(t *testing.T) {
	t.Parallel()

	config := DefaultGateConfig()
	config.AllowWhileCrawling = true
	config.AllowWhileIndexing = true
	state := openGateState()
	state.CrawlQueueSize = 9
	state.IndexQueueSize = 9

	report := EvaluateGates(state, config)
	if !report.Open {
		t.Fatalf("Open = false, reason %q", report.BlockingReason)
	}
	assertOpen(t, report, GateCrawlIdle)
	assertOpen(t, report, GateIndexIdle)
}

func TestEvaluateGatesAllowsSingleIndexQueueItemLikeYaCy(t *testing.T) {
	t.Parallel()

	config := DefaultGateConfig()
	config.AllowWhileIndexing = false
	state := openGateState()
	state.IndexQueueSize = 1

	report := EvaluateGates(state, config)
	if !report.Open {
		t.Fatalf("Open = false, reason %q", report.BlockingReason)
	}
	assertOpen(t, report, GateIndexIdle)
}

func assertClosed(t *testing.T, report GateReport, name GateName, reason string) {
	t.Helper()

	result, ok := gateByName(report, name)
	if !ok {
		t.Fatalf("missing gate %q", name)
	}
	if result.Open || result.Reason != reason {
		t.Fatalf("gate %q = %#v, want closed reason %q", name, result, reason)
	}
}

func assertOpen(t *testing.T, report GateReport, name GateName) {
	t.Helper()

	result, ok := gateByName(report, name)
	if !ok {
		t.Fatalf("missing gate %q", name)
	}
	if !result.Open || result.Reason != GateOpenReason {
		t.Fatalf("gate %q = %#v, want open", name, result)
	}
}

func gateByName(report GateReport, name GateName) (GateResult, bool) {
	for _, result := range report.Results {
		if result.Name == name {
			return result, true
		}
	}

	return GateResult{}, false
}
