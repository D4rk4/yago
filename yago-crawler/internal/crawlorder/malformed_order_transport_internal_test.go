package crawlorder

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type malformedReconnectExchange struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	mutex         sync.Mutex
	registrations int
	reconnected   chan struct{}
	reconnectOnce sync.Once
	leaseID       string
	orderData     []byte
}

func (exchange *malformedReconnectExchange) StreamOrders(
	_ *crawlrpc.WorkerRegistration,
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
) error {
	exchange.mutex.Lock()
	exchange.registrations++
	attempt := exchange.registrations
	exchange.mutex.Unlock()
	if attempt == 1 {
		if err := stream.Send(&crawlrpc.CrawlOrderMessage{
			LeaseId: exchange.leaseID, OrderJson: []byte("{"),
		}); err != nil {
			return fmt.Errorf("send malformed crawl order: %w", err)
		}
		<-stream.Context().Done()

		return status.FromContextError(stream.Context().Err()).Err()
	}
	exchange.reconnectOnce.Do(func() { close(exchange.reconnected) })
	if err := stream.Send(&crawlrpc.CrawlOrderMessage{
		LeaseId:           exchange.leaseID,
		OrderJson:         exchange.orderData,
		Recovered:         true,
		RecoveredBatchEnd: true,
		RecoveredLeaseIds: []string{exchange.leaseID},
	}); err != nil {
		return fmt.Errorf("send recovered crawl order: %w", err)
	}
	<-stream.Context().Done()

	return status.FromContextError(stream.Context().Err()).Err()
}

func (*malformedReconnectExchange) AckOrder(
	context.Context,
	*crawlrpc.OrderAck,
) (*crawlrpc.OrderAckResult, error) {
	return nil, status.Error(codes.InvalidArgument, "malformed settlement rejected")
}

func (*malformedReconnectExchange) Heartbeat(
	_ context.Context,
	heartbeat *crawlrpc.WorkerHeartbeat,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	return &crawlrpc.WorkerHeartbeatResult{
		RenewedLeaseIds:      append([]string(nil), heartbeat.GetActiveLeaseIds()...),
		LeaseTtlMilliseconds: uint64(time.Minute / time.Millisecond),
	}, nil
}

func TestMalformedSettlementFailureReconnectsAndRecoversLeaseOverTransport(t *testing.T) {
	fastRetry(t)
	orderData, err := yagocrawlcontract.MarshalCrawlOrder(
		yagocrawlcontract.CrawlOrder{
			Profile: yagocrawlcontract.NewCrawlProfile(
				yagocrawlcontract.CrawlProfile{Name: "recovered-after-malformed"},
			),
		},
	)
	if err != nil {
		t.Fatalf("marshal recovered order: %v", err)
	}
	exchange := &malformedReconnectExchange{
		reconnected: make(chan struct{}),
		leaseID:     "malformed-recovered-over-transport",
		orderData:   orderData,
	}
	client := newMalformedReconnectClient(t, exchange)
	ctx, cancel := context.WithCancel(context.Background())
	registry := crawllease.NewGrantRegistry(ctx, 1)
	receiver := NewGRPCOrderReceiver(
		ctx,
		client,
		"worker",
		nil,
		WithWorkerLeaseSession("session", registry),
	)
	delivery := awaitOrder(t, receiver)
	if delivery.LeaseID != exchange.leaseID ||
		delivery.Order.Profile.Name != "recovered-after-malformed" {
		t.Fatalf("recovered delivery = %+v", delivery)
	}
	select {
	case <-exchange.reconnected:
	default:
		t.Fatal("malformed settlement failure did not reconnect the order stream")
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func newMalformedReconnectClient(
	t *testing.T,
	exchange *malformedReconnectExchange,
) crawlrpc.CrawlExchangeClient {
	t.Helper()
	var listenerConfig net.ListenConfig
	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for malformed reconnect transport: %v", err)
	}
	serverCredentials, clientCredentials := ephemeralTLSTransportCredentials(t)
	server := grpc.NewServer(grpc.Creds(serverCredentials))
	crawlrpc.RegisterCrawlExchangeServer(server, exchange)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-serveDone
	})
	connection, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(clientCredentials),
	)
	if err != nil {
		t.Fatalf("connect malformed reconnect transport: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return crawlrpc.NewCrawlExchangeClient(connection)
}
