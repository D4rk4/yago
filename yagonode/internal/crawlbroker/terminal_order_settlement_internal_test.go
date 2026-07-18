package crawlbroker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func terminalOrderAcknowledgment(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	workerID string,
	requeue bool,
) *crawlrpc.OrderAck {
	t.Helper()
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found {
		t.Fatalf("lease %q is missing", leaseID)
	}
	workerSessionID := workerID + "-session"
	record.WorkerSessionID = workerSessionID
	if err := queue.vault.Update(t.Context(), func(transaction *vault.Txn) error {
		if err := queue.leases.Put(transaction, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("store terminal lease session: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("bind terminal lease session: %v", err)
	}
	identity := sha256.Sum256(record.OrderData)
	rate := uint32(30)

	return &crawlrpc.OrderAck{
		LeaseId:         leaseID,
		Requeue:         requeue,
		OrderIdentity:   identity[:],
		WorkerId:        workerID,
		WorkerSessionId: workerSessionID,
		TerminalState:   crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
		TerminalTally:   &crawlrpc.CrawlRunTally{Fetched: 3, Indexed: 2, Failed: 1},
		PagesPerMinute:  &rate,
	}
}

func copyTerminalOrderAcknowledgment(source *crawlrpc.OrderAck) *crawlrpc.OrderAck {
	copy := &crawlrpc.OrderAck{
		LeaseId:           source.GetLeaseId(),
		Requeue:           source.GetRequeue(),
		OrderIdentity:     append([]byte(nil), source.GetOrderIdentity()...),
		WorkerId:          source.GetWorkerId(),
		WorkerSessionId:   source.GetWorkerSessionId(),
		ConfirmationToken: append([]byte(nil), source.GetConfirmationToken()...),
		TerminalState:     source.GetTerminalState(),
		TerminalTally: &crawlrpc.CrawlRunTally{
			Fetched:      source.GetTerminalTally().GetFetched(),
			Indexed:      source.GetTerminalTally().GetIndexed(),
			Failed:       source.GetTerminalTally().GetFailed(),
			RobotsDenied: source.GetTerminalTally().GetRobotsDenied(),
			Duplicates:   source.GetTerminalTally().GetDuplicates(),
			Pending:      source.GetTerminalTally().GetPending(),
		},
	}
	if source.PagesPerMinute != nil {
		rate := source.GetPagesPerMinute()
		copy.PagesPerMinute = &rate
	}

	return copy
}

type exactTerminalAcknowledgmentFixture struct {
	queue   *DurableOrderQueue
	leaseID string
	request *crawlrpc.OrderAck
	sink    *recordingProgressSink
	server  *exchangeServer
}

func TestTerminalOrderAcknowledgmentIsExactAndIdempotent(t *testing.T) {
	fixture := newExactTerminalAcknowledgmentFixture(t)
	token := acknowledgeTerminalOrderTwice(t, fixture)
	assertTerminalOrderProgressRecorded(t, fixture)
	assertTerminalOrderMutationsRejected(t, fixture)
	confirmTerminalOrderTwice(t, fixture, token)
}

func newExactTerminalAcknowledgmentFixture(t *testing.T) exactTerminalAcknowledgmentFixture {
	t.Helper()
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-exact", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	sink := &recordingProgressSink{}
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	server.progress = sink

	return exactTerminalAcknowledgmentFixture{
		queue: queue, leaseID: leaseID, request: request, sink: sink, server: server,
	}
}

func acknowledgeTerminalOrderTwice(
	t *testing.T,
	fixture exactTerminalAcknowledgmentFixture,
) []byte {
	t.Helper()
	var token []byte
	for attempt := 0; attempt < 2; attempt++ {
		result, err := fixture.server.AckOrder(t.Context(), fixture.request)
		if err != nil {
			t.Fatalf("terminal acknowledgment %d: %v", attempt, err)
		}
		if len(result.GetConfirmationToken()) != sha256.Size {
			t.Fatalf("terminal token %d length = %d", attempt, len(result.GetConfirmationToken()))
		}
		if token == nil {
			token = append([]byte(nil), result.GetConfirmationToken()...)
		} else if !bytes.Equal(token, result.GetConfirmationToken()) {
			t.Fatal("terminal acknowledgment token changed across retry")
		}
	}

	return token
}

func assertTerminalOrderProgressRecorded(t *testing.T, fixture exactTerminalAcknowledgmentFixture) {
	t.Helper()
	if fixture.sink.n != 1 || fixture.sink.last.WorkerID != "worker" ||
		fixture.sink.last.Tally.Fetched != 3 || !fixture.sink.last.RateKnown ||
		fixture.sink.last.PagesPerMinute != 30 {
		t.Fatalf("terminal progress = %d/%+v", fixture.sink.n, fixture.sink.last)
	}
	if _, found := leaseRecordFor(t, fixture.queue, fixture.leaseID); found {
		t.Fatal("terminal acknowledgment retained lease")
	}
	if err := fixture.queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, err := fixture.queue.leaseSettlements.Get(tx, vault.Key(fixture.leaseID))
		if err != nil || !found || !record.Terminal || !record.ProgressDelivered {
			t.Fatalf("terminal tombstone = %+v, found=%v err=%v", record, found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read terminal tombstone: %v", err)
	}
}

func assertTerminalOrderMutationsRejected(
	t *testing.T,
	fixture exactTerminalAcknowledgmentFixture,
) {
	t.Helper()
	mutations := []func(*crawlrpc.OrderAck){
		func(changed *crawlrpc.OrderAck) { changed.Requeue = true },
		func(changed *crawlrpc.OrderAck) { changed.WorkerId = "stale" },
		func(changed *crawlrpc.OrderAck) { changed.WorkerSessionId = "stale-session" },
		func(changed *crawlrpc.OrderAck) { changed.OrderIdentity[0] ^= 0xff },
		func(changed *crawlrpc.OrderAck) {
			changed.TerminalState = crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED
		},
		func(changed *crawlrpc.OrderAck) { changed.TerminalTally.Fetched++ },
		func(changed *crawlrpc.OrderAck) { (*changed.PagesPerMinute)++ },
	}
	for index, mutate := range mutations {
		changed := copyTerminalOrderAcknowledgment(fixture.request)
		mutate(changed)
		if _, err := fixture.server.AckOrder(
			t.Context(),
			changed,
		); status.Code(
			err,
		) != codes.FailedPrecondition {
			t.Fatalf("mutation %d status = %v, want failed precondition", index, status.Code(err))
		}
	}
}

func confirmTerminalOrderTwice(
	t *testing.T,
	fixture exactTerminalAcknowledgmentFixture,
	token []byte,
) {
	t.Helper()
	confirmation := copyTerminalOrderAcknowledgment(fixture.request)
	confirmation.ConfirmationToken = append([]byte(nil), token...)
	for attempt := 0; attempt < 2; attempt++ {
		result, err := fixture.server.AckOrder(t.Context(), confirmation)
		if err != nil || len(result.GetConfirmationToken()) != 0 {
			t.Fatalf("terminal confirmation %d = %+v, %v", attempt, result, err)
		}
	}
	invalidConfirmation := copyTerminalOrderAcknowledgment(confirmation)
	invalidConfirmation.TerminalTally.Fetched++
	if _, err := fixture.server.AckOrder(
		t.Context(),
		invalidConfirmation,
	); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mutated terminal confirmation status = %v", status.Code(err))
	}
	if err := fixture.queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		if _, found, err := fixture.queue.leaseSettlements.Get(
			tx,
			vault.Key(fixture.leaseID),
		); err != nil ||
			found {
			t.Fatalf("confirmed terminal tombstone found=%v err=%v", found, err)
		}
		if _, found, err := fixture.queue.completedControlTargets.Get(
			tx,
			vault.Key(fixture.leaseID),
		); err != nil || found {
			t.Fatalf("confirmed terminal control target found=%v err=%v", found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read confirmed terminal cleanup: %v", err)
	}
}

func TestTerminalConfirmationTokenSurvivesBrokerRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open terminal token storage: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open terminal token queue: %v", err)
	}
	leaseID := leaseOne(t, queue, "terminal-token-restart", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	server := newExchangeServer(queue, nil)
	result, err := server.AckOrder(t.Context(), request)
	if err != nil || len(result.GetConfirmationToken()) != sha256.Size {
		t.Fatalf("initial terminal token = %+v, %v", result, err)
	}
	confirmation := copyTerminalOrderAcknowledgment(request)
	confirmation.ConfirmationToken = append([]byte(nil), result.GetConfirmationToken()...)
	if err := storage.Close(); err != nil {
		t.Fatalf("close terminal token storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen terminal token storage: %v", err)
	}
	defer func() { _ = storage.Close() }()
	queue, err = newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("reopen terminal token queue: %v", err)
	}
	server = newExchangeServer(queue, nil)
	if _, err := server.AckOrder(t.Context(), confirmation); err != nil {
		t.Fatalf("confirm terminal token after restart: %v", err)
	}
	if err := queue.vault.View(t.Context(), func(transaction *vault.Txn) error {
		if _, found, err := queue.leaseSettlements.Get(
			transaction,
			vault.Key(leaseID),
		); err != nil || found {
			t.Fatalf("terminal token restart tombstone found=%v err=%v", found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read terminal token restart cleanup: %v", err)
	}
}

func TestTerminalOrderAcknowledgmentRejectsIncompleteDefinitions(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-invalid", "worker")
	valid := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	cases := []*crawlrpc.OrderAck{
		{
			LeaseId:       leaseID,
			WorkerId:      "worker",
			TerminalState: crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
		},
		copyTerminalOrderAcknowledgment(valid),
		copyTerminalOrderAcknowledgment(valid),
		copyTerminalOrderAcknowledgment(valid),
		copyTerminalOrderAcknowledgment(valid),
		copyTerminalOrderAcknowledgment(valid),
		copyTerminalOrderAcknowledgment(valid),
		copyTerminalOrderAcknowledgment(valid),
		copyTerminalOrderAcknowledgment(valid),
	}
	cases[1].OrderIdentity = []byte("short")
	cases[2].WorkerId = ""
	cases[3].WorkerSessionId = ""
	cases[4].TerminalState = crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING
	cases[5].TerminalTally.Pending = 1
	cases[6].LeaseId = strings.Repeat("l", yagocrawlcontract.MaximumCrawlLeaseIDBytes+1)
	cases[7].WorkerId = strings.Repeat("w", yagocrawlcontract.MaximumCrawlerWorkerIdentityBytes+1)
	cases[8].WorkerSessionId = strings.Repeat(
		"s",
		yagocrawlcontract.MaximumCrawlerSessionIdentityBytes+1,
	)
	for index, request := range cases {
		server := newExchangeServer(queue, nil)
		if _, err := server.AckOrder(
			t.Context(),
			request,
		); status.Code(
			err,
		) != codes.InvalidArgument {
			t.Fatalf("invalid definition %d status = %v", index, status.Code(err))
		}
	}
}

func TestTerminalOrderAcknowledgmentFencesExpiredOwner(t *testing.T) {
	set := withClock(t)
	base := time.Unix(40_000, 0)
	set(base)
	queue := memQueue(t)
	oldLeaseID := leaseOne(t, queue, "terminal-expired", "worker-old")
	request := terminalOrderAcknowledgment(t, queue, oldLeaseID, "worker-old", false)
	set(base.Add(DefaultLeaseTTL))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("expire old lease: %v", err)
	}
	sink := &recordingProgressSink{}
	server := newExchangeServer(queue, nil)
	server.progress = sink
	if _, err := server.AckOrder(
		t.Context(),
		request,
	); status.Code(
		err,
	) != codes.FailedPrecondition {
		t.Fatalf("expired owner status = %v, want failed precondition", status.Code(err))
	}
	if sink.n != 0 {
		t.Fatalf("expired owner progress records = %d", sink.n)
	}
	if _, found := leaseRecordFor(t, queue, oldLeaseID); !found {
		t.Fatal("expired owner removed parked lease")
	}
}

func TestTerminalProgressDeliveryFailureLeavesSettlementPending(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-delivery-failure", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	sink := &recordingProgressSink{terminalErr: context.Canceled}
	server := newExchangeServer(queue, nil)
	server.progress = sink
	if _, err := server.AckOrder(t.Context(), request); status.Code(err) != codes.Internal {
		t.Fatalf("failed delivery status = %v, want internal", status.Code(err))
	}
	if sink.n != 0 {
		t.Fatalf("failed delivery records = %d", sink.n)
	}
	if err := queue.vault.View(t.Context(), func(transaction *vault.Txn) error {
		settlement, found, err := queue.leaseSettlements.Get(transaction, vault.Key(leaseID))
		if err != nil || !found || settlement.ProgressDelivered {
			t.Fatalf("pending terminal settlement = %+v, found=%v err=%v", settlement, found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read pending terminal settlement: %v", err)
	}
	sink.terminalErr = nil
	if _, err := server.AckOrder(t.Context(), request); err != nil {
		t.Fatalf("retry terminal delivery: %v", err)
	}
	if sink.n != 1 || !bytes.Equal(sink.identity, request.GetOrderIdentity()) {
		t.Fatalf("retried terminal delivery = %d/%x", sink.n, sink.identity)
	}
}

func TestTerminalProgressConfirmationFailureDoesNotRedeliver(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-confirmation-failure", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	sink := &recordingProgressSink{confirmErr: context.Canceled}
	server := newExchangeServer(queue, nil)
	server.progress = sink
	if _, err := server.AckOrder(t.Context(), request); status.Code(err) != codes.Internal {
		t.Fatalf("failed confirmation status = %v, want internal", status.Code(err))
	}
	if sink.n != 1 || sink.confirmN != 0 {
		t.Fatalf("failed confirmation delivery counts = %d/%d", sink.n, sink.confirmN)
	}
	if err := queue.vault.View(t.Context(), func(transaction *vault.Txn) error {
		settlement, found, err := queue.leaseSettlements.Get(transaction, vault.Key(leaseID))
		if err != nil || !found || !settlement.ProgressDelivered {
			t.Fatalf("flagged terminal settlement = %+v, found=%v err=%v", settlement, found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read flagged terminal settlement: %v", err)
	}
	sink.confirmErr = nil
	if _, err := server.AckOrder(t.Context(), request); err != nil {
		t.Fatalf("retry terminal confirmation: %v", err)
	}
	if sink.n != 1 || sink.confirmN != 1 {
		t.Fatalf("retried confirmation delivery counts = %d/%d", sink.n, sink.confirmN)
	}
}

type terminalBrokerCrashFixture struct {
	path           string
	leaseID        string
	acknowledgment *crawlrpc.OrderAck
	settlement     leaseSettlementRecord
}

func TestTerminalOrderProgressReconcilesAfterBrokerCrash(t *testing.T) {
	fixture := recordTerminalRunBeforeBrokerFlag(t)
	replayTerminalRunAndCommitBrokerFlag(t, fixture)
	assertFlaggedTerminalRunDoesNotRedeliver(t, fixture)
}

func recordTerminalRunBeforeBrokerFlag(t *testing.T) terminalBrokerCrashFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first storage: %v", err)
	}
	firstQueue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open first queue: %v", err)
	}
	firstRuns, err := crawlruns.Open(t.Context(), storage, 8)
	if err != nil {
		t.Fatalf("open first run registry: %v", err)
	}
	leaseID := leaseOne(t, firstQueue, "terminal-crash", "worker")
	acknowledgment := terminalOrderAcknowledgment(t, firstQueue, leaseID, "worker", false)
	request, rich, err := terminalLeaseRequestFromProto(acknowledgment)
	if err != nil || !rich {
		t.Fatalf("decode terminal request: rich=%v err=%v", rich, err)
	}
	settlement, err := firstQueue.prepareTerminalLeaseSettlement(t.Context(), leaseID, request)
	if err != nil || settlement.ProgressDelivered {
		t.Fatalf("prepare terminal settlement = %+v, %v", settlement, err)
	}
	if err := firstRuns.RecordTerminal(
		t.Context(),
		settlement.OrderIdentity,
		settlement.Progress,
	); err != nil {
		t.Fatalf("commit terminal run before broker flag: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close after terminal run commit: %v", err)
	}

	return terminalBrokerCrashFixture{
		path: path, leaseID: leaseID, acknowledgment: acknowledgment, settlement: settlement,
	}
}

func replayTerminalRunAndCommitBrokerFlag(t *testing.T, fixture terminalBrokerCrashFixture) {
	t.Helper()
	storage, err := boltvault.Open(fixture.path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	secondQueue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open second queue: %v", err)
	}
	secondRuns, err := crawlruns.Open(t.Context(), storage, 8)
	if err != nil {
		t.Fatalf("open second run registry: %v", err)
	}
	secondObservations := 0
	secondRuns.AddObserver(func(crawlruns.Run, bool, int) { secondObservations++ })
	if err := secondRuns.RecordTerminal(
		t.Context(),
		fixture.settlement.OrderIdentity,
		fixture.settlement.Progress,
	); err != nil {
		t.Fatalf("replay committed terminal delivery: %v", err)
	}
	if secondObservations != 0 {
		t.Fatalf("committed delivery replay observations = %d", secondObservations)
	}
	if err := secondQueue.acknowledgeTerminalProgress(
		t.Context(),
		fixture.leaseID,
		fixture.settlement,
	); err != nil {
		t.Fatalf("commit broker delivery flag: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close after broker delivery flag: %v", err)
	}
}

func assertFlaggedTerminalRunDoesNotRedeliver(t *testing.T, fixture terminalBrokerCrashFixture) {
	t.Helper()
	storage, err := boltvault.Open(fixture.path, 0)
	if err != nil {
		t.Fatalf("reopen after broker delivery flag: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	thirdQueue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open third queue: %v", err)
	}
	thirdRuns, err := crawlruns.Open(t.Context(), storage, 8)
	if err != nil {
		t.Fatalf("open third run registry: %v", err)
	}
	thirdObservations := 0
	thirdRuns.AddObserver(func(crawlruns.Run, bool, int) { thirdObservations++ })
	server := newExchangeServer(thirdQueue, nil)
	server.progress = thirdRuns
	if _, err := server.AckOrder(context.Background(), fixture.acknowledgment); err != nil {
		t.Fatalf("replay after broker delivery flag: %v", err)
	}
	if thirdObservations != 0 {
		t.Fatalf("flagged delivery replay observations = %d", thirdObservations)
	}
	runs := thirdRuns.Recent()
	if len(runs) != 1 || runs[0].RunID != testOrderRunID || runs[0].Tally.Fetched != 3 {
		t.Fatalf("durable terminal run = %+v", runs)
	}
}

func TestTerminalRequeueIsIdempotent(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-requeue", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", true)
	request.TerminalState = crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED
	sink := &recordingProgressSink{}
	server := newExchangeServer(queue, nil)
	server.progress = sink
	var token []byte
	for attempt := 0; attempt < 2; attempt++ {
		result, err := server.AckOrder(t.Context(), request)
		if err != nil {
			t.Fatalf("terminal requeue %d: %v", attempt, err)
		}
		if token == nil {
			token = append([]byte(nil), result.GetConfirmationToken()...)
		} else if !bytes.Equal(token, result.GetConfirmationToken()) {
			t.Fatal("terminal requeue token changed across retry")
		}
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || !record.Deferred || record.WorkerID != "" {
		t.Fatalf("deferred terminal lease = %+v, found=%v", record, found)
	}
	if sink.n != 1 || sink.last.State != yagocrawlcontract.CrawlRunCancelled {
		t.Fatalf("terminal requeue progress = %d/%+v", sink.n, sink.last)
	}
	confirmation := copyTerminalOrderAcknowledgment(request)
	confirmation.ConfirmationToken = token
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := server.AckOrder(t.Context(), confirmation); err != nil {
			t.Fatalf("terminal requeue confirmation %d: %v", attempt, err)
		}
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth != (QueueDepth{Pending: 1}) {
		t.Fatalf("confirmed terminal requeue depth = %+v, %v", depth, err)
	}
}

func TestTerminalRequeueConfirmationDoesNotTouchReplacementLease(t *testing.T) {
	set := withClock(t)
	base := time.Unix(50_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-requeue-replacement", "worker-old")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker-old", true)
	request.TerminalState = crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED
	server := newExchangeServer(queue, nil)
	result, err := server.AckOrder(t.Context(), request)
	if err != nil {
		t.Fatalf("prepare replacement requeue: %v", err)
	}
	set(base.Add(negativeAcknowledgmentRetryDelay))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep deferred terminal lease: %v", err)
	}
	_, replacementLeaseID, found, err := queue.leasePopForSession(
		t.Context(),
		"worker-new",
		"session-new",
	)
	if err != nil || !found || replacementLeaseID == leaseID {
		t.Fatalf("replacement terminal lease = %q, found=%v err=%v", replacementLeaseID, found, err)
	}
	confirmation := copyTerminalOrderAcknowledgment(request)
	confirmation.ConfirmationToken = append([]byte(nil), result.GetConfirmationToken()...)
	if _, err := server.AckOrder(t.Context(), confirmation); err != nil {
		t.Fatalf("confirm old terminal requeue: %v", err)
	}
	replacement, found := leaseRecordFor(t, queue, replacementLeaseID)
	if !found || replacement.WorkerID != "worker-new" ||
		replacement.WorkerSessionID != "session-new" {
		t.Fatalf("replacement terminal lease = %+v, found=%v", replacement, found)
	}
}
