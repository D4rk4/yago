package ingest

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type IngestBatch = yagocrawlcontract.IngestBatch

const (
	DefaultIngestRetryWait = 100 * time.Millisecond
	maximumIngestRetryWait = 5 * time.Second
	msgIngestBackpressure  = "crawl ingest delayed by node backpressure"
)

// IngestSubmitter is the slice of the node's CrawlExchange client the publisher
// needs: a unary submission that blocks until the node absorbs the batch.
type IngestSubmitter interface {
	SubmitIngest(
		ctx context.Context,
		in *crawlrpc.IngestBatchMessage,
		opts ...grpc.CallOption,
	) (*crawlrpc.IngestAck, error)
}

type GRPCIngestPublisher struct {
	client    IngestSubmitter
	retryWait time.Duration
	workerID  string
	sessionID string
	grants    *crawllease.GrantRegistry
}

type GRPCIngestPublisherOption func(*GRPCIngestPublisher)

func WithIngestLeaseSession(
	workerID string,
	sessionID string,
	grants *crawllease.GrantRegistry,
) GRPCIngestPublisherOption {
	return func(publisher *GRPCIngestPublisher) {
		publisher.workerID = workerID
		publisher.sessionID = sessionID
		publisher.grants = grants
	}
}

func NewGRPCIngestPublisher(
	client IngestSubmitter,
	options ...GRPCIngestPublisherOption,
) *GRPCIngestPublisher {
	publisher := &GRPCIngestPublisher{
		client:    client,
		retryWait: DefaultIngestRetryWait,
	}
	for _, apply := range options {
		apply(publisher)
	}

	return publisher
}

func (p *GRPCIngestPublisher) Publish(ctx context.Context, batch IngestBatch) error {
	data, err := prepareIngestMessage(batch)
	if err != nil {
		return fmt.Errorf("prepare ingest batch %s: %w", batch.SourceURL, err)
	}
	leaseID := crawllease.LeaseID(ctx)
	if p.grants != nil && leaseID == "" {
		return fmt.Errorf("submit ingest batch %s: %w", batch.SourceURL, crawllease.ErrLeaseLost)
	}
	msg := &crawlrpc.IngestBatchMessage{
		BatchJson:       data,
		LeaseId:         leaseID,
		WorkerId:        p.workerID,
		WorkerSessionId: p.sessionID,
	}
	retryWait := p.retryWait
	retries := 0
	for {
		_, err := p.client.SubmitIngest(ctx, msg)
		if err == nil {
			return nil
		}
		if status.Code(err) == codes.FailedPrecondition {
			if p.grants != nil {
				p.grants.Revoke(leaseID)
			}

			return fmt.Errorf(
				"submit ingest batch %s: %w",
				batch.SourceURL,
				errors.Join(crawllease.ErrLeaseLost, err),
			)
		}
		if !retryableIngestStatus(status.Code(err)) {
			return fmt.Errorf("submit ingest batch %s: %w", batch.SourceURL, err)
		}
		retries++
		if retries == 1 {
			slog.WarnContext(
				ctx,
				msgIngestBackpressure,
				slog.String("sourceUrl", batch.SourceURL),
				slog.Int("payloadBytes", len(data)),
			)
		}
		timer := time.NewTimer(jitteredIngestRetryWait(retryWait, cryptorand.Reader))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("submit ingest batch %s: %w", batch.SourceURL, ctx.Err())
		}
		retryWait = min(maximumIngestRetryWait, retryWait*2)
	}
}

func retryableIngestStatus(code codes.Code) bool {
	return code == codes.Unavailable || code == codes.ResourceExhausted
}

func jitteredIngestRetryWait(wait time.Duration, entropy io.Reader) time.Duration {
	half := wait / 2
	offset, err := cryptorand.Int(entropy, big.NewInt(int64(wait-half)))
	if err != nil {
		return half
	}

	return half + time.Duration(offset.Int64())
}
