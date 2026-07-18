package crawlbroker

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

const transportFlowOrderTotal = 270

const (
	transportFlowWorkerID        = "transport-worker"
	transportFlowWorkerSessionID = "transport-session"
)

type transportFlowReceiver struct {
	stream       crawlrpc.CrawlExchange_StreamOrdersClient
	current      *crawlrpc.CrawlOrderMessage
	nextDelivery <-chan *crawlrpc.CrawlOrderMessage
	nextFailure  <-chan error
}

func TestGRPCTransportKeepsControlRPCsLiveDuringOrderFlood(t *testing.T) {
	client := newTransportFlowClient(t)
	receiver := newTransportFlowReceiver(t, client)
	assertTransportDeliveryCreditHeld(t, receiver)
	activeLeaseIDs := receiveTransportOrderFlood(t, client, receiver)
	renewTransportLeaseSet(t, client, activeLeaseIDs)
	settleTransportLease(t, client, activeLeaseIDs[0])
	reportTransportProgress(t, client, activeLeaseIDs[1])
}

func newTransportFlowClient(t *testing.T) crawlrpc.CrawlExchangeClient {
	t.Helper()
	storage, err := boltvault.Open(filepath.Join(t.TempDir(), "crawlbroker.db"), 0)
	if err != nil {
		t.Fatalf("open crawl broker storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open crawl order queue: %v", err)
	}
	for index := range transportFlowOrderTotal {
		if err := queue.Publish(
			t.Context(),
			testOrder(fmt.Sprintf("transport-%03d", index)),
		); err != nil {
			t.Fatalf("publish crawl order %d: %v", index, err)
		}
	}

	server := newExchangeServer(queue, nil)
	var listenerConfig net.ListenConfig
	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for crawl exchange: %v", err)
	}
	serverCredentials, clientCredentials := newEphemeralLoopbackTLSCredentials(t)
	grpcServer := grpc.NewServer(grpc.Creds(serverCredentials))
	crawlrpc.RegisterCrawlExchangeServer(grpcServer, server)
	serveDone := make(chan error, 1)
	go func() { serveDone <- grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
		<-serveDone
	})
	connection, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(clientCredentials),
	)
	if err != nil {
		t.Fatalf("connect crawl exchange: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return crawlrpc.NewCrawlExchangeClient(connection)
}

func newTransportFlowReceiver(
	t *testing.T,
	client crawlrpc.CrawlExchangeClient,
) *transportFlowReceiver {
	t.Helper()
	streamContext, cancelStream := context.WithCancel(t.Context())
	t.Cleanup(cancelStream)
	stream, err := client.StreamOrders(streamContext, &crawlrpc.WorkerRegistration{
		WorkerId: transportFlowWorkerID, WorkerSessionId: transportFlowWorkerSessionID,
	})
	if err != nil {
		t.Fatalf("stream crawl orders: %v", err)
	}

	current, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive first crawl order: %v", err)
	}
	nextDelivery := make(chan *crawlrpc.CrawlOrderMessage, 1)
	nextFailure := make(chan error, 1)
	go func() {
		next, receiveErr := stream.Recv()
		if receiveErr != nil {
			nextFailure <- receiveErr

			return
		}
		nextDelivery <- next
	}()

	return &transportFlowReceiver{
		stream:       stream,
		current:      current,
		nextDelivery: nextDelivery,
		nextFailure:  nextFailure,
	}
}

func assertTransportDeliveryCreditHeld(t *testing.T, receiver *transportFlowReceiver) {
	t.Helper()
	select {
	case next := <-receiver.nextDelivery:
		t.Fatalf("order %q bypassed delivery credit", next.GetLeaseId())
	case err := <-receiver.nextFailure:
		t.Fatalf("order stream failed before confirmation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
}

func receiveTransportOrderFlood(
	t *testing.T,
	client crawlrpc.CrawlExchangeClient,
	receiver *transportFlowReceiver,
) []string {
	t.Helper()
	activeLeaseIDs := make([]string, 0, transportFlowOrderTotal)
	seenLeaseIDs := make(map[string]struct{}, transportFlowOrderTotal)
	for index := range transportFlowOrderTotal {
		leaseID := receiver.current.GetLeaseId()
		if _, duplicate := seenLeaseIDs[leaseID]; duplicate {
			t.Fatalf("duplicate crawl lease %q", leaseID)
		}
		seenLeaseIDs[leaseID] = struct{}{}
		activeLeaseIDs = append(activeLeaseIDs, leaseID)
		confirmTransportLease(t, client, leaseID)
		if index%32 == 0 {
			reportTransportProgress(t, client, leaseID)
		}
		if index+1 == transportFlowOrderTotal {
			break
		}
		receiver.current = receiveNextTransportOrder(t, receiver, index)
	}

	return activeLeaseIDs
}

func receiveNextTransportOrder(
	t *testing.T,
	receiver *transportFlowReceiver,
	index int,
) *crawlrpc.CrawlOrderMessage {
	t.Helper()
	if index == 0 {
		select {
		case next := <-receiver.nextDelivery:
			return next
		case err := <-receiver.nextFailure:
			t.Fatalf("receive confirmed crawl order: %v", err)
		case <-time.After(time.Second):
			t.Fatal("confirmed crawl order was not delivered")
		}
	}
	next, err := receiver.stream.Recv()
	if err != nil {
		t.Fatalf("receive crawl order %d: %v", index+1, err)
	}

	return next
}

func renewTransportLeaseSet(
	t *testing.T,
	client crawlrpc.CrawlExchangeClient,
	activeLeaseIDs []string,
) {
	t.Helper()
	heartbeatContext, cancelHeartbeat := context.WithTimeout(t.Context(), time.Second)
	result, err := client.Heartbeat(heartbeatContext, &crawlrpc.WorkerHeartbeat{
		WorkerId:        transportFlowWorkerID,
		WorkerSessionId: transportFlowWorkerSessionID,
		ActiveLeaseIds:  activeLeaseIDs,
	})
	cancelHeartbeat()
	if err != nil || len(result.GetRenewedLeaseIds()) != transportFlowOrderTotal {
		t.Fatalf(
			"full crawl heartbeat renewed=%d error=%v",
			len(result.GetRenewedLeaseIds()),
			err,
		)
	}
}

func settleTransportLease(
	t *testing.T,
	client crawlrpc.CrawlExchangeClient,
	leaseID string,
) {
	t.Helper()
	acknowledgmentContext, cancelAcknowledgment := context.WithTimeout(t.Context(), time.Second)
	_, err := client.AckOrder(acknowledgmentContext, &crawlrpc.OrderAck{
		LeaseId:         leaseID,
		WorkerId:        transportFlowWorkerID,
		WorkerSessionId: transportFlowWorkerSessionID,
	})
	cancelAcknowledgment()
	if err != nil {
		t.Fatalf("settle crawl order after flood: %v", err)
	}
}

func confirmTransportLease(
	t *testing.T,
	client crawlrpc.CrawlExchangeClient,
	leaseID string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := client.Heartbeat(ctx, &crawlrpc.WorkerHeartbeat{
		WorkerId:        transportFlowWorkerID,
		WorkerSessionId: transportFlowWorkerSessionID,
		ActiveLeaseIds:  []string{leaseID},
	})
	if err != nil || len(result.GetRenewedLeaseIds()) != 1 ||
		result.GetRenewedLeaseIds()[0] != leaseID {
		t.Fatalf(
			"confirm crawl lease %q renewed=%v error=%v",
			leaseID,
			result.GetRenewedLeaseIds(),
			err,
		)
	}
}

func reportTransportProgress(
	t *testing.T,
	client crawlrpc.CrawlExchangeClient,
	leaseID string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := client.ReportProgress(ctx, &crawlrpc.CrawlProgressReport{
		WorkerId:        transportFlowWorkerID,
		WorkerSessionId: transportFlowWorkerSessionID,
		LeaseId:         leaseID,
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); err != nil {
		t.Fatalf("report crawl progress for %q: %v", leaseID, err)
	}
}
