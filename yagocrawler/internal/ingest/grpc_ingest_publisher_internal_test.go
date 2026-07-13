package ingest

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"testing/iotest"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type fakeSubmitter struct {
	responses []error
	index     int
	calls     int
	deadlines []bool
}

func (f *fakeSubmitter) SubmitIngest(
	ctx context.Context,
	_ *crawlrpc.IngestBatchMessage,
	_ ...grpc.CallOption,
) (*crawlrpc.IngestAck, error) {
	f.calls++
	_, deadline := ctx.Deadline()
	f.deadlines = append(f.deadlines, deadline)
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

func testBatch() yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{SourceURL: "https://example.org/a"}
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
	if client.deadlines[0] {
		t.Fatal("publisher added an ambiguous per-attempt deadline")
	}
}

func TestGRPCIngestPublisherRejectsInvalidBatchBeforeSubmit(t *testing.T) {
	client := &fakeSubmitter{}
	publisher := NewGRPCIngestPublisher(client)
	err := publisher.Publish(context.Background(), IngestBatch{
		Document: yagocrawlcontract.DocumentIngest{DateConfidence: math.NaN()},
	})
	if err == nil {
		t.Fatal("expected invalid batch error")
	}
	if client.calls != 0 {
		t.Fatalf("calls = %d, want 0", client.calls)
	}
}

func TestGRPCIngestPublisherRetriesOnSaturation(t *testing.T) {
	client := &fakeSubmitter{responses: []error{
		status.Error(codes.Unavailable, "pipeline full"),
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

func TestGRPCIngestPublisherRetriesLegacySaturationCode(t *testing.T) {
	client := &fakeSubmitter{responses: []error{
		status.Error(codes.ResourceExhausted, "legacy pipeline full"),
		nil,
	}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Millisecond}
	if err := publisher.Publish(context.Background(), testBatch()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want two attempts", client.calls)
	}
}

func TestGRPCIngestPublisherHonorsContext(t *testing.T) {
	client := &fakeSubmitter{responses: []error{status.Error(codes.Unavailable, "full")}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, testBatch()); err == nil {
		t.Fatal("expected cancellation error while retrying")
	}
}

func TestJitteredIngestRetryWait(t *testing.T) {
	wait := 100 * time.Millisecond
	if got := jitteredIngestRetryWait(wait, bytes.NewReader(make([]byte, 8))); got != wait/2 {
		t.Fatalf("zero entropy wait = %s, want %s", got, wait/2)
	}
	if got := jitteredIngestRetryWait(
		wait,
		iotest.ErrReader(errors.New("entropy unavailable")),
	); got != wait/2 {
		t.Fatalf("fallback wait = %s, want %s", got, wait/2)
	}
}
