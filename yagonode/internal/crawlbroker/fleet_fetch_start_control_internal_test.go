package crawlbroker

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestFleetFetchStartControlBindsRestoredAuthoritativeRate(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		make(chan crawlresults.IngestDelivery),
		crawlerControlDefaults{processPagesPerSecond: 17, processRateSet: true},
	)
	control := newControlRegistry(crawlerControlDefaults{
		processPagesPerSecond: 23,
		processRateSet:        true,
	})
	if err := server.bindControl(control); err != nil {
		t.Fatalf("bind control: %v", err)
	}
	if snapshot := server.fetchStarts.Snapshot(); snapshot.PagesPerSecond != 23 {
		t.Fatalf("bound rate = %d, want restored 23", snapshot.PagesPerSecond)
	}
	control.SetProcessPagesPerSecond(27)
	if snapshot := server.fetchStarts.Snapshot(); snapshot.PagesPerSecond != 27 {
		t.Fatalf("updated rate = %d, want 27", snapshot.PagesPerSecond)
	}
	control.bindFleetFetchStarts(nil)
	control.SetProcessPagesPerSecond(29)
	if snapshot := server.fetchStarts.Snapshot(); snapshot.PagesPerSecond != 27 {
		t.Fatalf("unbound rate = %d, want 27", snapshot.PagesPerSecond)
	}
}

func TestFleetFetchStartControlRejectsInvalidRestoredAuthority(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		nil,
		crawlerControlDefaults{processPagesPerSecond: 7, processRateSet: true},
	)
	originalControl := server.control
	invalidControl := newControlRegistry()
	invalidControl.processPagesPerSecond = yagocrawlcontract.MaximumProcessPagesPerSecond + 1
	if err := server.bindControl(invalidControl); !errors.Is(err, errFleetFetchPolicyInvalid) {
		t.Fatalf("invalid restored authority error = %v", err)
	}
	if server.control != originalControl || server.fetchStarts.Snapshot().PagesPerSecond != 7 {
		t.Fatalf("invalid authority replaced live state: control=%p rate=%d",
			server.control, server.fetchStarts.Snapshot().PagesPerSecond)
	}
}

func TestFleetFetchStartControlRejectsUnavailablePolicy(t *testing.T) {
	defaults := crawlerControlDefaults{
		processPagesPerSecond: yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
		processRateSet:        true,
	}
	if _, err := newExchangeServerChecked(memQueue(t), nil, defaults); !errors.Is(
		err,
		errFleetFetchPolicyInvalid,
	) {
		t.Fatalf("invalid exchange policy error = %v", err)
	}
	defer func() {
		recovered, isError := recover().(error)
		if !isError || !errors.Is(recovered, errFleetFetchPolicyInvalid) {
			t.Fatalf("unchecked invalid exchange panic = %v", recovered)
		}
	}()
	newExchangeServer(memQueue(t), nil, defaults)
}

func TestFleetFetchStartControlFailsClosedWithoutSchedule(t *testing.T) {
	server := newExchangeServer(memQueue(t), nil)
	server.fetchStarts = nil
	if err := server.bindControl(newControlRegistry()); !errors.Is(
		err,
		errFleetFetchPolicyInvalid,
	) {
		t.Fatalf("bind unavailable policy error = %v", err)
	}
	if err := newExchangeServer(memQueue(t), nil).bindControl(nil); !errors.Is(
		err,
		errFleetFetchPolicyInvalid,
	) {
		t.Fatalf("bind nil control error = %v", err)
	}
	if err := server.setFleetPagesPerSecond(1); !errors.Is(err, errFleetFetchPolicyInvalid) {
		t.Fatalf("set unavailable policy error = %v", err)
	}
	if err := server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "worker", WorkerSessionId: "session", FetchStartLeases: true,
	}, &fakeOrderStream{ctx: t.Context()}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unavailable stream status = %v", status.Code(err))
	}
	validServer := newExchangeServer(memQueue(t), nil)
	if err := validServer.setFleetPagesPerSecond(
		yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
	); !errors.Is(err, errFleetFetchPolicyInvalid) {
		t.Fatalf("invalid policy rate error = %v", err)
	}
}

func TestFleetFetchStartControlPreservesRateWhenBindingFails(t *testing.T) {
	control := newControlRegistry(crawlerControlDefaults{
		processPagesPerSecond: 11,
		processRateSet:        true,
	})
	control.bindFleetFetchStarts(func(uint32) error {
		return errFleetFetchPolicyInvalid
	})
	if signalled := control.SetProcessPagesPerSecond(19); signalled != 0 {
		t.Fatalf("failed policy update signalled %d workers", signalled)
	}
	if rate := control.ProcessPagesPerSecond(); rate != 11 {
		t.Fatalf("rate after failed policy update = %d, want 11", rate)
	}
}

func TestFleetFetchStartControlFencesUnlimitedSessions(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	legacyDone := startFleetFetchOrderStream(t, server, "legacy", false)
	capableDone := startFleetFetchOrderStream(t, server, "capable", true)
	server.control.SetProcessPagesPerSecond(10)
	requireFleetFetchStreamStopped(t, legacyDone)
	requireFleetFetchStreamStopped(t, capableDone)
	if got := server.sessions.registration("legacy"); got.connected {
		t.Fatalf("legacy session remained connected: %+v", got)
	}
	if got := server.sessions.registration("capable"); got.connected {
		t.Fatalf("capable session remained connected: %+v", got)
	}
}

func TestFleetFetchStartControlRetainsCapableFiniteSession(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		make(chan crawlresults.IngestDelivery),
		crawlerControlDefaults{processPagesPerSecond: 10, processRateSet: true},
	)
	legacyContext, cancelLegacy := context.WithCancel(t.Context())
	legacyDone := make(chan error, 1)
	go func() {
		legacyDone <- server.StreamOrders(&crawlrpc.WorkerRegistration{
			WorkerId: "legacy", WorkerSessionId: "legacy-session",
		}, &fakeOrderStream{ctx: legacyContext})
	}()
	requireFleetFetchStreamStatus(t, legacyDone, codes.FailedPrecondition)
	capableDone := startFleetFetchOrderStream(t, server, "capable", true)
	server.control.SetProcessPagesPerSecond(20)
	select {
	case err := <-capableDone:
		t.Fatalf("capable finite session stopped: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancelLegacy()
	registration := server.sessions.registration("capable")
	registration.cancel()
	requireFleetFetchStreamStopped(t, capableDone)
}

func TestFleetFetchStartControlFencesCapableSessionOnFiniteDecrease(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		make(chan crawlresults.IngestDelivery),
		crawlerControlDefaults{processPagesPerSecond: 20, processRateSet: true},
	)
	done := startFleetFetchOrderStream(t, server, "capable", true)
	server.control.SetProcessPagesPerSecond(10)
	requireFleetFetchStreamStopped(t, done)
	if got := server.sessions.registration("capable"); got.connected {
		t.Fatalf("rate-decrease session remained connected: %+v", got)
	}
}

func startFleetFetchOrderStream(
	t *testing.T,
	server *exchangeServer,
	workerID string,
	fetchStartLeases bool,
) <-chan error {
	t.Helper()
	connectedBefore := server.control.RuntimeSnapshot().ConnectedCrawlers
	streamContext, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	workerSessionID := workerID + "-session"
	go func() {
		done <- server.StreamOrders(&crawlrpc.WorkerRegistration{
			WorkerId: workerID, WorkerSessionId: workerSessionID,
			FetchStartLeases: fetchStartLeases,
		}, &fakeOrderStream{ctx: streamContext})
	}()
	waitWorkerSession(t, server.sessions, workerID, workerSessionID)
	waitConnectedCrawlers(t, server.control, connectedBefore+1)

	return done
}

func requireFleetFetchStreamStopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("fetch-start stream stopped without fencing error")
		}
	case <-time.After(time.Second):
		t.Fatal("fetch-start stream was not fenced")
	}
}

func requireFleetFetchStreamStatus(
	t *testing.T,
	done <-chan error,
	want codes.Code,
) {
	t.Helper()
	select {
	case err := <-done:
		if got := status.Code(err); got != want {
			t.Fatalf("stream status = %v, want %v: %v", got, want, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("stream did not return status %v", want)
	}
}
