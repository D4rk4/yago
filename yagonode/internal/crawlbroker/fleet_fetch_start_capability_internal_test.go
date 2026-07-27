package crawlbroker

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

// TestFleetFetchStartCapabilityRefusalNamesItselfAndReleasesTheSession pins the
// capability admission that keeps a metered fleet metered. StreamOrders collapses
// both errFleetFetchPolicyInvalid and errFleetFetchCapabilityRequired into one
// FailedPrecondition status, so a status-code assertion cannot tell an
// unconfigured broker apart from a crawler that simply does not implement
// fetch-start leases; only the sentinel distinguishes them. The refusal also has
// to unwind the session it just activated: the worker is told to reconnect, and a
// registration left behind would answer that reconnect with errWorkerSessionActive
// and lock the crawler out until the session retention window expired.
func TestFleetFetchStartCapabilityRefusalNamesItselfAndReleasesTheSession(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	if err := server.setFleetPagesPerSecond(1); err != nil {
		t.Fatalf("meter the fleet: %v", err)
	}
	_, _, err := server.activateWorkerSession(
		context.Background(),
		"legacy",
		"legacy-session",
		func() {},
	)
	if !errors.Is(err, errFleetFetchCapabilityRequired) {
		t.Fatalf("metered legacy activation error = %v, want capability required", err)
	}
	if errors.Is(err, errFleetFetchPolicyInvalid) {
		t.Fatalf("capability refusal reported an unavailable policy: %v", err)
	}
	if current := server.sessions.registration("legacy"); current.connected {
		t.Fatalf("refused legacy session stayed connected: %+v", current)
	}
	if snapshot := server.fetchStarts.Snapshot(); snapshot.ActiveSessionTotal != 0 {
		t.Fatalf("refused legacy session joined the schedule: %+v", snapshot)
	}
	if _, _, err := server.activateWorkerSession(
		context.Background(),
		"legacy",
		"legacy-session",
		func() {},
		true,
	); err != nil {
		t.Fatalf("capable reconnect after refusal: %v", err)
	}
}

// TestFleetFetchStartCapabilityIsRequiredOnlyByAMeteredFleet pairs the two sides
// of the rate boundary. An unmetered fleet hands out unlimited permits, so a
// crawler that predates fetch-start leases is still safe to admit; the first
// non-zero page rate is where its unmetered fetching would break the fleet
// budget. Testing only the refusal would leave a broker free to reject every
// legacy crawler even with metering switched off.
func TestFleetFetchStartCapabilityIsRequiredOnlyByAMeteredFleet(t *testing.T) {
	unmetered := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	if _, _, err := unmetered.activateWorkerSession(
		context.Background(),
		"legacy",
		"legacy-session",
		func() {},
	); err != nil {
		t.Fatalf("unmetered legacy activation: %v", err)
	}
	metered := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	if err := metered.setFleetPagesPerSecond(1); err != nil {
		t.Fatalf("meter the fleet: %v", err)
	}
	if _, _, err := metered.activateWorkerSession(
		context.Background(),
		"capable",
		"capable-session",
		func() {},
		true,
	); err != nil {
		t.Fatalf("metered capable activation: %v", err)
	}
	if _, _, err := metered.activateWorkerSession(
		context.Background(),
		"legacy",
		"legacy-session",
		func() {},
	); !errors.Is(err, errFleetFetchCapabilityRequired) {
		t.Fatalf("metered legacy activation error = %v, want capability required", err)
	}
}
