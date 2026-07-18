package crawlbroker

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

type malformedOrderExchange struct {
	crawlrpc.CrawlExchangeServer
	malformed sync.Once
}

type malformedOrderStream struct {
	crawlrpc.CrawlExchange_StreamOrdersServer
	malformed *sync.Once
}

func (exchange *malformedOrderExchange) StreamOrders(
	registration *crawlrpc.WorkerRegistration,
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
) error {
	if err := exchange.CrawlExchangeServer.StreamOrders(
		registration,
		&malformedOrderStream{
			CrawlExchange_StreamOrdersServer: stream,
			malformed:                        &exchange.malformed,
		},
	); err != nil {
		return fmt.Errorf("stream malformed crawl order: %w", err)
	}

	return nil
}

func (stream *malformedOrderStream) Send(message *crawlrpc.CrawlOrderMessage) error {
	delivery := &crawlrpc.CrawlOrderMessage{
		OrderJson:         append([]byte(nil), message.GetOrderJson()...),
		LeaseId:           message.GetLeaseId(),
		Recovered:         message.GetRecovered(),
		RecoveredBatchEnd: message.GetRecoveredBatchEnd(),
		RecoveredLeaseIds: append([]string(nil), message.GetRecoveredLeaseIds()...),
	}
	stream.malformed.Do(func() { delivery.OrderJson = []byte("{") })

	if err := stream.CrawlExchange_StreamOrdersServer.Send(delivery); err != nil {
		return fmt.Errorf("send malformed crawl order: %w", err)
	}

	return nil
}

func TestMalformedOrdinaryAcknowledgmentReleasesTransportDeliveryCredit(t *testing.T) {
	client := newMalformedOrderTransportClient(t)
	streamContext, cancelStream := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelStream()
	stream, err := client.StreamOrders(streamContext, &crawlrpc.WorkerRegistration{
		WorkerId: "malformed-worker", WorkerSessionId: "malformed-session",
	})
	if err != nil {
		t.Fatalf("stream malformed crawl order: %v", err)
	}
	malformed, err := stream.Recv()
	if err != nil || string(malformed.GetOrderJson()) != "{" {
		t.Fatalf("malformed delivery = %q, %v", malformed.GetOrderJson(), err)
	}
	if _, err := client.AckOrder(streamContext, &crawlrpc.OrderAck{
		LeaseId:         malformed.GetLeaseId(),
		WorkerId:        "malformed-worker",
		WorkerSessionId: "malformed-session",
	}); err != nil {
		t.Fatalf("acknowledge malformed delivery: %v", err)
	}
	next, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive delivery after malformed acknowledgment: %v", err)
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(next.GetOrderJson())
	if err != nil || order.Profile.Name != "valid-after-malformed" {
		t.Fatalf("delivery after malformed acknowledgment = %+v, %v", order, err)
	}
}

func newMalformedOrderTransportClient(t *testing.T) crawlrpc.CrawlExchangeClient {
	t.Helper()
	storage, err := boltvault.Open(filepath.Join(t.TempDir(), "crawlbroker.db"), 0)
	if err != nil {
		t.Fatalf("open malformed transport storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open malformed transport queue: %v", err)
	}
	for _, name := range []string{"malformed-on-wire", "valid-after-malformed"} {
		if err := queue.Publish(t.Context(), testOrder(name)); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}
	inner := newExchangeServer(queue, nil)
	exchange := &malformedOrderExchange{CrawlExchangeServer: inner}
	var listenerConfig net.ListenConfig
	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for malformed transport: %v", err)
	}
	serverCredentials, clientCredentials := newEphemeralLoopbackTLSCredentials(t)
	grpcServer := grpc.NewServer(grpc.Creds(serverCredentials))
	crawlrpc.RegisterCrawlExchangeServer(grpcServer, exchange)
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
		t.Fatalf("connect malformed transport: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return crawlrpc.NewCrawlExchangeClient(connection)
}
