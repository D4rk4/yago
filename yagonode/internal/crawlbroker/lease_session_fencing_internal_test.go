package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type workerSessionSupersessionFixture struct {
	queue   *DurableOrderQueue
	server  *exchangeServer
	ingest  chan crawlresults.IngestDelivery
	leaseID string
	before  leaseRecord
	sink    *recordingProgressSink
}

func TestWorkerSessionSupersessionFencesLeaseOperations(t *testing.T) {
	fixture := newWorkerSessionSupersessionFixture(t)
	assertWorkerSessionLeaseAdoption(t, fixture)
	assertWorkerSessionHeartbeatFencing(t, fixture)
	assertWorkerSessionProgressFencing(t, fixture)
	assertSupersededSessionIngestRejected(t, fixture)
	assertCurrentSessionIngestAccepted(t, fixture)
}

func newWorkerSessionSupersessionFixture(t *testing.T) workerSessionSupersessionFixture {
	t.Helper()
	set := withClock(t)
	base := time.Unix(40_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	ingest := make(chan crawlresults.IngestDelivery, 1)
	server := newExchangeServer(queue, ingest)
	leaseID := leaseOneForSession(t, queue, "session", "worker", "session-a")
	activateTestWorkerSession(t, server, "worker", "session-a")
	before, found := leaseRecordFor(t, queue, leaseID)
	if !found {
		t.Fatal("initial lease missing")
	}
	deactivateTestWorkerSession(t, server, "session-a")
	activateTestWorkerSession(t, server, "worker", "session-b")
	sink := &recordingProgressSink{}
	server.progress = sink

	return workerSessionSupersessionFixture{
		queue: queue, server: server, ingest: ingest, leaseID: leaseID, before: before, sink: sink,
	}
}

func assertWorkerSessionLeaseAdoption(t *testing.T, fixture workerSessionSupersessionFixture) {
	t.Helper()
	after, found := leaseRecordFor(t, fixture.queue, fixture.leaseID)
	if !found || after.WorkerSessionID != "session-b" ||
		after.ExpiresAtUnixNano != fixture.before.ExpiresAtUnixNano {
		t.Fatalf("adopted lease = %#v/%v, want session-b and unchanged deadline", after, found)
	}
}

func assertWorkerSessionHeartbeatFencing(t *testing.T, fixture workerSessionSupersessionFixture) {
	t.Helper()
	if _, err := fixture.server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: "session-a", ActiveLeaseIds: []string{fixture.leaseID},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("superseded heartbeat status = %v, want FailedPrecondition", status.Code(err))
	}
	heartbeat, err := fixture.server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: "session-b", ActiveLeaseIds: []string{fixture.leaseID},
	})
	if err != nil || len(heartbeat.GetRenewedLeaseIds()) != 1 ||
		heartbeat.GetRenewedLeaseIds()[0] != fixture.leaseID {
		t.Fatalf("current heartbeat = %+v, err=%v", heartbeat, err)
	}
}

func assertWorkerSessionProgressFencing(t *testing.T, fixture workerSessionSupersessionFixture) {
	t.Helper()
	if _, err := fixture.server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId: fixture.leaseID, WorkerId: "worker", WorkerSessionId: "session-a",
		RunId: []byte("admin"), State: crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("superseded progress status = %v, want FailedPrecondition", status.Code(err))
	}
	if fixture.sink.n != 0 {
		t.Fatalf("superseded progress records = %d, want 0", fixture.sink.n)
	}
	if _, err := fixture.server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId: fixture.leaseID, WorkerId: "worker", WorkerSessionId: "session-b",
		RunId: []byte("admin"), State: crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); err != nil {
		t.Fatalf("current progress: %v", err)
	}
	if fixture.sink.n != 1 {
		t.Fatalf("current progress records = %d, want 1", fixture.sink.n)
	}
}

func assertSupersededSessionIngestRejected(t *testing.T, fixture workerSessionSupersessionFixture) {
	t.Helper()
	staleMessage := leaseIngestMessage(t, fixture.leaseID, "session-a")
	staleDone := make(chan error, 1)
	go func() {
		_, submitErr := fixture.server.SubmitIngest(t.Context(), staleMessage)
		staleDone <- submitErr
	}()
	staleDelivery := <-fixture.ingest
	if err := staleDelivery.AuthorizeLeaseSnapshot(t.Context()); err == nil {
		t.Fatal("superseded ingest mutation was authorized")
	}
	if err := staleDelivery.LeaseLost(t.Context()); err != nil {
		t.Fatalf("settle superseded ingest: %v", err)
	}
	if err := <-staleDone; status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("superseded ingest status = %v, want FailedPrecondition", status.Code(err))
	}
}

func assertCurrentSessionIngestAccepted(t *testing.T, fixture workerSessionSupersessionFixture) {
	t.Helper()
	currentMessage := leaseIngestMessage(t, fixture.leaseID, "session-b")
	currentDone := make(chan error, 1)
	go func() {
		_, submitErr := fixture.server.SubmitIngest(t.Context(), currentMessage)
		currentDone <- submitErr
	}()
	currentDelivery := <-fixture.ingest
	if err := currentDelivery.AuthorizeLeaseSnapshot(t.Context()); err != nil {
		t.Fatalf("authorize current ingest: %v", err)
	}
	if err := currentDelivery.Ack(t.Context()); err != nil {
		t.Fatalf("ack current ingest: %v", err)
	}
	if err := <-currentDone; err != nil {
		t.Fatalf("current ingest: %v", err)
	}
}

func TestExpiredUnreclaimedLeaseCannotRenewOrMutate(t *testing.T) {
	set := withClock(t)
	base := time.Unix(50_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	leaseID := leaseOneForSession(t, queue, "expired", "worker", testWorkerSessionID)
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	set(base.Add(time.Minute))
	result, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		ActiveLeaseIds: []string{leaseID},
	})
	if err != nil {
		t.Fatalf("expired heartbeat: %v", err)
	}
	if len(result.GetRenewedLeaseIds()) != 0 {
		t.Fatalf("expired heartbeat renewed %v", result.GetRenewedLeaseIds())
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || record.ExpiresAtUnixNano != base.Add(time.Minute).UnixNano() {
		t.Fatalf("expired unswept lease changed = %#v/%v", record, found)
	}
	if _, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId: leaseID, WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
		RunId: []byte("admin"), State: crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expired progress status = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestLeaseExpiringBetweenRenewalReadAndWriteIsNotResurrected(t *testing.T) {
	set := withClock(t)
	base := time.Unix(55_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOneForSession(t, queue, "renewal-stall", "worker", testWorkerSessionID)
	set(base.Add(20 * time.Second))
	previousHook := beforeLeaseRenewalWrite
	t.Cleanup(func() { beforeLeaseRenewalWrite = previousHook })
	beforeLeaseRenewalWrite = func() { set(base.Add(time.Minute)) }
	renewed, _, err := queue.renewLeases(
		t.Context(),
		"worker",
		testWorkerSessionID,
		[]string{leaseID},
	)
	if err != nil {
		t.Fatalf("renew stalled lease: %v", err)
	}
	if len(renewed) != 0 {
		t.Fatalf("expired stalled renewals = %v", renewed)
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || record.ExpiresAtUnixNano != base.Add(time.Minute).UnixNano() {
		t.Fatalf("stalled renewal resurrected lease = %#v/%v", record, found)
	}
}

func TestLeaseMutationLinearizesBeforeSessionAdoption(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	leaseID := leaseOneForSession(t, queue, "linear", "worker", "session-a")
	activateTestWorkerSession(t, server, "worker", "session-a")
	release, err := queue.beginAuthorizedLeaseMutation(t.Context(), leaseAuthorization{
		LeaseID: leaseID, WorkerID: "worker", WorkerSessionID: "session-a", RunID: "61646d696e",
	})
	if err != nil {
		t.Fatalf("authorize mutation: %v", err)
	}
	deactivateTestWorkerSession(t, server, "session-a")
	adopted := make(chan error, 1)
	go func() {
		_, _, activationErr := server.activateWorkerSession(
			context.Background(),
			"worker",
			"session-b",
			func() {},
		)
		adopted <- activationErr
	}()
	select {
	case err := <-adopted:
		t.Fatalf("session adoption crossed active mutation fence: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	release()
	select {
	case err := <-adopted:
		if err != nil {
			t.Fatalf("adopt session: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session adoption remained blocked after mutation")
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || record.WorkerSessionID != "session-b" {
		t.Fatalf("post-mutation lease = %#v/%v, want session-b", record, found)
	}
}

func TestNoRefreshHeartbeatRunsDuringIngestMutation(t *testing.T) {
	set := withClock(t)
	base := time.Unix(56_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOneForSession(t, queue, "read-renewal", "worker", "session")
	release, err := queue.beginAuthorizedLeaseMutation(t.Context(), leaseAuthorization{
		LeaseID: leaseID, WorkerID: "worker", WorkerSessionID: "session",
		RunID: "61646d696e",
	})
	if err != nil {
		t.Fatalf("authorize ingest mutation: %v", err)
	}
	renewed := make(chan error, 1)
	go func() {
		leaseIDs, _, renewalErr := queue.renewLeases(
			context.Background(), "worker", "session", []string{leaseID},
		)
		if renewalErr == nil && (len(leaseIDs) != 1 || leaseIDs[0] != leaseID) {
			renewalErr = fmt.Errorf("renewed leases = %v", leaseIDs)
		}
		renewed <- renewalErr
	}()
	select {
	case err := <-renewed:
		if err != nil {
			release()
			t.Fatalf("no-refresh heartbeat during ingest: %v", err)
		}
	case <-time.After(time.Second):
		release()
		t.Fatal("no-refresh heartbeat blocked behind ingest mutation")
	}
	release()
}

func TestLeaseMutationGroupDoesNotDeadlockBehindWaitingSessionAdoption(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	firstLeaseID := leaseOneForSession(t, queue, "first", "worker", "session-a")
	secondLeaseID := leaseOneForSession(t, queue, "second", "worker", "session-a")
	activateTestWorkerSession(t, server, "worker", "session-a")
	groupContext, releaseGroup := queue.beginAuthorizedLeaseMutationGroup(t.Context())
	firstRelease, err := queue.beginAuthorizedLeaseMutation(groupContext, leaseAuthorization{
		LeaseID: firstLeaseID, WorkerID: "worker", WorkerSessionID: "session-a",
		RunID: "61646d696e",
	})
	if err != nil {
		releaseGroup()
		t.Fatalf("authorize first grouped mutation: %v", err)
	}
	deactivateTestWorkerSession(t, server, "session-a")
	adopted := make(chan error, 1)
	go func() {
		_, _, activationErr := server.activateWorkerSession(
			context.Background(), "worker", "session-b", func() {},
		)
		adopted <- activationErr
	}()
	select {
	case err := <-adopted:
		firstRelease()
		releaseGroup()
		t.Fatalf("session adoption crossed active mutation group: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	secondDone := make(chan error, 1)
	var secondRelease func()
	go func() {
		var authorizationErr error
		secondRelease, authorizationErr = queue.beginAuthorizedLeaseMutation(
			groupContext,
			leaseAuthorization{
				LeaseID: secondLeaseID, WorkerID: "worker", WorkerSessionID: "session-a",
				RunID: "61646d696e",
			},
		)
		secondDone <- authorizationErr
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			firstRelease()
			releaseGroup()
			t.Fatalf("authorize second grouped mutation: %v", err)
		}
	case <-time.After(time.Second):
		firstRelease()
		releaseGroup()
		t.Fatal("second grouped mutation deadlocked behind waiting session adoption")
	}
	secondRelease()
	firstRelease()
	releaseGroup()
	select {
	case err := <-adopted:
		if err != nil {
			t.Fatalf("adopt session after grouped mutation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session adoption remained blocked after grouped mutation")
	}
	for _, leaseID := range []string{firstLeaseID, secondLeaseID} {
		record, found := leaseRecordFor(t, queue, leaseID)
		if !found || record.WorkerSessionID != "session-b" {
			t.Fatalf("adopted grouped lease %s = %#v/%v", leaseID, record, found)
		}
	}
}

func TestWorkerSessionCapacityReclaimsOnlyExpiredInactiveEntries(t *testing.T) {
	base := time.Unix(60_000, 0)
	now := base
	registry := newWorkerSessionRegistry(1, time.Minute)
	registry.now = func() time.Time { return now }
	generation, err := registry.activate("worker-a", "session-a", func() {}, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("activate worker-a: %v", err)
	}
	registry.deactivate("worker-a", "stale-session", generation)
	current := registry.registration("worker-a")
	if !current.connected {
		t.Fatal("stale stream release deactivated current session")
	}
	registry.deactivate("worker-a", "session-a", generation)
	now = base.Add(30 * time.Second)
	if !registry.current("worker-a", "session-a") {
		t.Fatal("inactive session heartbeat was rejected before retention")
	}
	now = base.Add(time.Minute)
	if _, err := registry.activate("worker-b", "session-b", func() {}, func() error {
		return nil
	}); err == nil {
		t.Fatal("recently active disconnected session was evicted")
	}
	now = base.Add(90 * time.Second)
	if _, err := registry.activate("worker-b", "session-b", func() {}, func() error {
		return nil
	}); err != nil {
		t.Fatalf("reclaim expired inactive session: %v", err)
	}
	if registry.current("worker-a", "session-a") {
		t.Fatal("expired inactive session remained current")
	}
}

func TestSameSessionReconnectFencesOldStreamGeneration(t *testing.T) {
	base := time.Unix(70_000, 0)
	now := base
	registry := newWorkerSessionRegistry(1, time.Minute)
	registry.now = func() time.Time { return now }
	firstGeneration, err := registry.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate first stream: %v", err)
	}
	if _, err := registry.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	); !errors.Is(err, errWorkerSessionActive) {
		t.Fatalf("duplicate stream error = %v", err)
	}
	registry.deactivate("worker", "session", firstGeneration)
	secondGeneration, err := registry.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate replacement stream: %v", err)
	}
	if firstGeneration == secondGeneration {
		t.Fatalf("stream generations = %d/%d", firstGeneration, secondGeneration)
	}
	registry.deactivate("worker", "session", firstGeneration)
	current := registry.registration("worker")
	if !current.connected || current.generation != secondGeneration {
		t.Fatalf("replacement session after stale release = %+v", current)
	}
	now = base.Add(2 * time.Minute)
	if _, err := registry.activate("other", "other-session", func() {}, func() error {
		return nil
	}); err == nil {
		t.Fatal("connected replacement was evicted after stale release")
	}
	registry.deactivate("worker", "session", secondGeneration)
	now = base.Add(3 * time.Minute)
	if _, err := registry.activate("other", "other-session", func() {}, func() error {
		return nil
	}); err != nil {
		t.Fatalf("reclaim released replacement: %v", err)
	}
}

func TestNewWorkerSessionCannotTakeOverLiveStream(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	aContext, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	aDone := make(chan error, 1)
	go func() {
		aDone <- server.StreamOrders(
			&crawlrpc.WorkerRegistration{WorkerId: "worker", WorkerSessionId: "session-a"},
			&fakeOrderStream{ctx: aContext},
		)
	}()
	waitWorkerSession(t, server.sessions, "worker", "session-a")
	err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{WorkerId: "worker", WorkerSessionId: "session-b"},
		&fakeOrderStream{ctx: context.Background()},
	)
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("takeover status = %v, want AlreadyExists", status.Code(err))
	}
	select {
	case err := <-aDone:
		t.Fatalf("live stream stopped during takeover: %v", err)
	default:
	}
	waitConnectedCrawlers(t, server.control, 1)
	if _, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: "session-a",
	}); err != nil {
		t.Fatalf("live stream heartbeat after takeover attempt: %v", err)
	}
	if _, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: "session-b",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rejected stream heartbeat status = %v", status.Code(err))
	}
	cancelA()
	select {
	case <-aDone:
	case <-time.After(time.Second):
		t.Fatal("live stream did not stop")
	}
	waitConnectedCrawlers(t, server.control, 0)
}

func TestBlockedWorkerSessionAdoptionDoesNotBlockAnotherWorkerHeartbeat(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker-b", "session-b")
	adoptionStarted := make(chan struct{})
	continueAdoption := make(chan struct{})
	adoptionDone := make(chan error, 1)
	go func() {
		_, err := server.sessions.activate(
			"worker-a",
			"session-a",
			func() {},
			func() error {
				close(adoptionStarted)
				<-continueAdoption

				return nil
			},
		)
		adoptionDone <- err
	}()
	select {
	case <-adoptionStarted:
	case <-time.After(time.Second):
		close(continueAdoption)
		t.Fatal("worker-a adoption did not start")
	}
	heartbeatDone := make(chan error, 1)
	go func() {
		_, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
			WorkerId: "worker-b", WorkerSessionId: "session-b",
		})
		heartbeatDone <- err
	}()
	select {
	case err := <-heartbeatDone:
		if err != nil {
			close(continueAdoption)
			t.Fatalf("worker-b heartbeat: %v", err)
		}
	case <-time.After(time.Second):
		close(continueAdoption)
		t.Fatal("worker-b heartbeat blocked behind worker-a adoption")
	}
	close(continueAdoption)
	select {
	case err := <-adoptionDone:
		if err != nil {
			t.Fatalf("finish worker-a adoption: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker-a adoption did not finish")
	}
}

func TestSameWorkerSessionReconnectsAfterPriorStreamCloses(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	firstContext, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- server.StreamOrders(
			&crawlrpc.WorkerRegistration{WorkerId: "worker", WorkerSessionId: "session"},
			&fakeOrderStream{ctx: firstContext},
		)
	}()
	waitWorkerSession(t, server.sessions, "worker", "session")
	first := server.sessions.registration("worker")
	firstGeneration := first.generation
	if err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{WorkerId: "worker", WorkerSessionId: "session"},
		&fakeOrderStream{ctx: context.Background()},
	); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate stream status = %v, want AlreadyExists", status.Code(err))
	}
	cancelFirst()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("prior stream did not stop")
	}
	secondContext, cancelSecond := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- server.StreamOrders(
			&crawlrpc.WorkerRegistration{WorkerId: "worker", WorkerSessionId: "session"},
			&fakeOrderStream{ctx: secondContext},
		)
	}()
	waitWorkerSessionGeneration(t, server.sessions, "worker", "session", firstGeneration)
	waitConnectedCrawlers(t, server.control, 1)
	if _, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "worker", WorkerSessionId: "session",
	}); err != nil {
		t.Fatalf("replacement heartbeat after stale release: %v", err)
	}
	cancelSecond()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("replacement stream did not stop")
	}
	waitConnectedCrawlers(t, server.control, 0)
}

func waitWorkerSession(
	t *testing.T,
	registry *workerSessionRegistry,
	workerID string,
	workerSessionID string,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !registry.current(workerID, workerSessionID) {
		if time.Now().After(deadline) {
			t.Fatalf("worker session %s/%s did not activate", workerID, workerSessionID)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitWorkerSessionGeneration(
	t *testing.T,
	registry *workerSessionRegistry,
	workerID string,
	workerSessionID string,
	priorGeneration uint64,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		current := registry.registration(workerID)
		if current.id == workerSessionID && current.connected &&
			current.generation != priorGeneration {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker session %s/%s did not reconnect", workerID, workerSessionID)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitConnectedCrawlers(t *testing.T, registry *ControlRegistry, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for registry.RuntimeSnapshot().ConnectedCrawlers != want {
		if time.Now().After(deadline) {
			t.Fatalf(
				"connected crawlers = %d, want %d",
				registry.RuntimeSnapshot().ConnectedCrawlers,
				want,
			)
		}
		time.Sleep(time.Millisecond)
	}
}

func leaseIngestMessage(
	t *testing.T,
	leaseID string,
	workerSessionID string,
) *crawlrpc.IngestBatchMessage {
	t.Helper()
	data, err := yagocrawlcontract.MarshalIngestBatch(yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org/session", Provenance: []byte("admin"),
		ProfileHandle: testOrder("session").Profile.Handle,
	})
	if err != nil {
		t.Fatalf("marshal ingest: %v", err)
	}

	return &crawlrpc.IngestBatchMessage{
		BatchJson: data, LeaseId: leaseID,
		WorkerId: "worker", WorkerSessionId: workerSessionID,
	}
}
