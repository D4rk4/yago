package crawlbroker

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type invalidCrawlerIdentity struct {
	name      string
	workerID  string
	sessionID string
}

func TestOversizedCrawlerIdentitiesAreRejectedBeforeMutation(t *testing.T) {
	identities := []invalidCrawlerIdentity{
		{
			name: "worker",
			workerID: strings.Repeat(
				"w",
				yagocrawlcontract.MaximumCrawlerWorkerIdentityBytes+1,
			),
			sessionID: "session",
		},
		{
			name:     "session",
			workerID: "worker",
			sessionID: strings.Repeat(
				"s",
				yagocrawlcontract.MaximumCrawlerSessionIdentityBytes+1,
			),
		},
	}
	for _, identity := range identities {
		t.Run(identity.name+"/stream", func(t *testing.T) {
			assertInvalidCrawlerStream(t, identity)
		})
		t.Run(identity.name+"/heartbeat", func(t *testing.T) {
			assertInvalidCrawlerHeartbeat(t, identity)
		})
		t.Run(identity.name+"/ack", func(t *testing.T) {
			assertInvalidCrawlerAcknowledgment(t, identity)
		})
		t.Run(identity.name+"/progress", func(t *testing.T) {
			assertInvalidCrawlerProgress(t, identity)
		})
		t.Run(identity.name+"/ingest", func(t *testing.T) {
			assertInvalidCrawlerIngest(t, identity)
		})
	}
}

func assertInvalidCrawlerStream(t *testing.T, identity invalidCrawlerIdentity) {
	t.Helper()
	queue := memQueue(t)
	if err := queue.Publish(t.Context(), testOrder("bounded")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	err := server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: identity.workerID, WorkerSessionId: identity.sessionID,
	}, &fakeOrderStream{ctx: t.Context()})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("stream status = %v, want InvalidArgument", status.Code(err))
	}
	if pendingCount(t, queue) != 1 || len(server.sessions.sessions) != 0 ||
		server.control.RuntimeSnapshot().ConnectedCrawlers != 0 {
		t.Fatal("invalid stream mutated broker state")
	}
}

func assertInvalidCrawlerHeartbeat(t *testing.T, identity invalidCrawlerIdentity) {
	t.Helper()
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "heartbeat", "worker", "session")
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", "session")
	before, _ := leaseRecordFor(t, queue, leaseID)
	_, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
		WorkerId: identity.workerID, WorkerSessionId: identity.sessionID,
		ActiveLeaseIds: []string{leaseID},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("heartbeat status = %v, want InvalidArgument", status.Code(err))
	}
	after, _ := leaseRecordFor(t, queue, leaseID)
	if after.ExpiresAtUnixNano != before.ExpiresAtUnixNano || after.WorkerID != before.WorkerID ||
		after.WorkerSessionID != before.WorkerSessionID {
		t.Fatal("invalid heartbeat mutated lease")
	}
}

func assertInvalidCrawlerAcknowledgment(t *testing.T, identity invalidCrawlerIdentity) {
	t.Helper()
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "ack", "worker", "session")
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	_, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: leaseID, WorkerId: identity.workerID, WorkerSessionId: identity.sessionID,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ack status = %v, want InvalidArgument", status.Code(err))
	}
	if _, found := leaseRecordFor(t, queue, leaseID); !found {
		t.Fatal("invalid acknowledgment removed lease")
	}
}

func assertInvalidCrawlerProgress(t *testing.T, identity invalidCrawlerIdentity) {
	t.Helper()
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "progress", "worker", "session")
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "worker", "session")
	sink := &recordingProgressSink{}
	server.progress = sink
	_, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId: leaseID, WorkerId: identity.workerID, WorkerSessionId: identity.sessionID,
		RunId: []byte("admin"), State: crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("progress status = %v, want InvalidArgument", status.Code(err))
	}
	if sink.n != 0 {
		t.Fatalf("invalid progress records = %d", sink.n)
	}
}

func assertInvalidCrawlerIngest(t *testing.T, identity invalidCrawlerIdentity) {
	t.Helper()
	ingest := make(chan crawlresults.IngestDelivery, 1)
	server := newExchangeServer(memQueue(t), ingest)
	message := leaseIngestMessage(t, "lease", identity.sessionID)
	message.WorkerId = identity.workerID
	_, err := server.SubmitIngest(t.Context(), message)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ingest status = %v, want InvalidArgument", status.Code(err))
	}
	if len(ingest) != 0 {
		t.Fatal("invalid ingest reached mutation queue")
	}
}
