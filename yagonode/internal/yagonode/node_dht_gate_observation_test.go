package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestDHTGateStateSnapshotPropagatesQueueDepths(t *testing.T) {
	source := dhtGateStateSource{
		storage:  capacityProbe{},
		postings: rwiCounter{count: 123},
		roster:   reachableRoster{},
		crawl: crawlQueueDepthSource{probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{Pending: 7, Leased: 2}, nil
		}},
		index: indexQueueDepthSource{probe: func() (int, bool) { return 4, true }},
	}

	state := source.Snapshot(t.Context())
	if state.CrawlQueueSize != 9 || !state.CrawlQueueKnown ||
		state.IndexQueueSize != 4 || !state.IndexQueueKnown {
		t.Fatalf("queue state = %+v", state)
	}
	if !state.LocalRWIKnown || !state.StorageKnown {
		t.Fatalf("storage state = %+v", state)
	}

	config := dhtexchange.DefaultGateConfig()
	config.MinimumConnectedPeer = 1
	config.AllowWhileCrawling = false
	report := dhtexchange.EvaluateGates(state, config)
	if result, _ := gateResultByName(report, dhtexchange.GateCrawlIdle); result.Open {
		t.Fatalf("crawl gate = %+v", result)
	}
}

func TestDHTGateStateSnapshotMarksReadFailuresUnknown(t *testing.T) {
	source := dhtGateStateSource{
		storage:  capacityProbe{err: errors.New("capacity failed")},
		postings: rwiCounter{err: errors.New("count failed")},
		roster:   reachableRoster{},
		crawl: crawlQueueDepthSource{probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{}, errors.New("depth failed")
		}},
		index: indexQueueDepthSource{probe: func() (int, bool) { return 0, false }},
	}

	state := source.Snapshot(t.Context())
	if state.LocalRWIKnown || state.CrawlQueueKnown || state.IndexQueueKnown || state.StorageKnown {
		t.Fatalf("state = %+v", state)
	}

	config := dhtexchange.DefaultGateConfig()
	config.AllowWhileIndexing = false
	report := dhtexchange.EvaluateGates(state, config)
	for name, reason := range map[dhtexchange.GateName]string{
		dhtexchange.GateLocalRWI:         dhtexchange.GateLocalRWIUnavailableReason,
		dhtexchange.GateCrawlIdle:        dhtexchange.GateCrawlQueueUnavailableReason,
		dhtexchange.GateIndexIdle:        dhtexchange.GateIndexQueueUnavailableReason,
		dhtexchange.GateStorageAvailable: dhtexchange.GateStorageStatusUnavailableReason,
	} {
		result, ok := gateResultByName(report, name)
		if !ok || result.Open || result.Reason != reason {
			t.Fatalf("gate %q = %+v, want closed reason %q", name, result, reason)
		}
	}
}

func gateResultByName(
	report dhtexchange.GateReport,
	name dhtexchange.GateName,
) (dhtexchange.GateResult, bool) {
	for _, result := range report.Results {
		if result.Name == name {
			return result, true
		}
	}

	return dhtexchange.GateResult{}, false
}
