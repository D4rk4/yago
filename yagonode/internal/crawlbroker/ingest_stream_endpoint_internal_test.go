package crawlbroker

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
)

func TestStreamIngestReturnsAcknowledgmentBeforeClientClosesSend(t *testing.T) {
	deliveries := make(chan crawlresults.IngestDelivery)
	server := newExchangeServer(memQueue(t), deliveries)
	message := ingestMessage(t, "https://example.org/stream")
	message.SubmissionId = "stream-submission"
	authorizeIngestMessage(t, server, message, "stream-ingest")
	go func() {
		delivery := <-deliveries
		if err := delivery.AuthorizeLeaseSnapshot(context.Background()); err == nil {
			_ = delivery.Ack(context.Background())
		} else {
			_ = delivery.LeaseLost(context.Background())
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for ingest stream: %v", err)
	}
	grpcServer := grpc.NewServer()
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
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("connect ingest stream: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	streamContext, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	stream, err := crawlrpc.NewCrawlExchangeClient(connection).StreamIngest(streamContext)
	if err != nil {
		t.Fatalf("open ingest stream: %v", err)
	}
	invalidMessage := *message
	invalidMessage.SubmissionId = ""
	if err := stream.Send(&invalidMessage); err != nil {
		t.Fatalf("send ingest without submission identity: %v", err)
	}
	invalidAcknowledgment, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive invalid ingest acknowledgment: %v", err)
	}
	if invalidAcknowledgment.GetSubmissionId() != "" ||
		invalidAcknowledgment.GetErrorCode() != int32(codes.InvalidArgument) {
		t.Fatalf("invalid ingest acknowledgment = %+v", invalidAcknowledgment)
	}
	if err := stream.Send(message); err != nil {
		t.Fatalf("send ingest batch: %v", err)
	}
	acknowledgment, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive acknowledgment before closing send: %v", err)
	}
	if acknowledgment.GetSubmissionId() != message.GetSubmissionId() ||
		acknowledgment.GetErrorCode() != 0 || acknowledgment.GetRejected() {
		t.Fatalf("ingest stream acknowledgment = %+v", acknowledgment)
	}
}
