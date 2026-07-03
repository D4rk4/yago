package ingest

import (
	"context"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
)

type fakeSubmitter struct {
	responses []error
	index     int
	calls     int
}

func (f *fakeSubmitter) SubmitIngest(
	_ context.Context,
	_ *crawlrpc.IngestBatchMessage,
	_ ...grpc.CallOption,
) (*crawlrpc.IngestAck, error) {
	f.calls++
	var err error
	if f.index < len(f.responses) {
		err = f.responses[f.index]
		f.index++
	}
	if err != nil {
		return nil, err
	}

	return &crawlrpc.IngestAck{}, nil
}

func testBatch() yacycrawlcontract.IngestBatch {
	return yacycrawlcontract.IngestBatch{SourceURL: "https://example.org/a"}
}

func TestGRPCIngestPublisherSubmitsBatch(t *testing.T) {
	client := &fakeSubmitter{}
	publisher := NewGRPCIngestPublisher(client)
	if err := publisher.Publish(context.Background(), testBatch()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("calls = %d, want 1", client.calls)
	}
}

func TestGRPCIngestPublisherRetriesOnSaturation(t *testing.T) {
	client := &fakeSubmitter{responses: []error{
		status.Error(codes.ResourceExhausted, "pipeline full"),
		nil,
	}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Millisecond}
	if err := publisher.Publish(context.Background(), testBatch()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want 2", client.calls)
	}
}

func TestGRPCIngestPublisherReturnsNonRetryableError(t *testing.T) {
	client := &fakeSubmitter{responses: []error{status.Error(codes.Internal, "boom")}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Millisecond}
	if err := publisher.Publish(context.Background(), testBatch()); err == nil {
		t.Fatal("expected non-retryable error to surface")
	}
}

func TestGRPCIngestPublisherHonorsContext(t *testing.T) {
	client := &fakeSubmitter{responses: []error{status.Error(codes.ResourceExhausted, "full")}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, testBatch()); err == nil {
		t.Fatal("expected cancellation error while retrying")
	}
}
