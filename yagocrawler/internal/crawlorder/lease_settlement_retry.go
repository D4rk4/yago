package crawlorder

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	DefaultLeaseSettlementRetryWait    = 100 * time.Millisecond
	DefaultLeaseSettlementShutdownWait = 5 * time.Second
	maximumLeaseSettlementRetryWait    = 5 * time.Second
	msgLeaseSettlementRetry            = "crawl order lease settlement retrying"
)

type leaseSettlementPolicy struct {
	retryWait        time.Duration
	maximumRetryWait time.Duration
	shutdownWait     time.Duration
}

type leaseSettlementSession struct {
	serviceCtx context.Context
	client     OrderStreamer
	request    *crawlrpc.OrderAck
	policy     leaseSettlementPolicy
}

func settleLease(
	serviceCtx context.Context,
	client OrderStreamer,
	leaseID string,
	requeue bool,
) func(context.Context) error {
	return settleLeaseWithPolicy(
		serviceCtx,
		client,
		leaseID,
		requeue,
		leaseSettlementPolicy{
			retryWait:        DefaultLeaseSettlementRetryWait,
			maximumRetryWait: maximumLeaseSettlementRetryWait,
			shutdownWait:     DefaultLeaseSettlementShutdownWait,
		},
	)
}

func settleLeaseWithPolicy(
	serviceCtx context.Context,
	client OrderStreamer,
	leaseID string,
	requeue bool,
	policy leaseSettlementPolicy,
) func(context.Context) error {
	return func(context.Context) error {
		if leaseID == "" {
			return fmt.Errorf("settle crawl order lease: empty lease id")
		}
		return leaseSettlementSession{
			serviceCtx: serviceCtx,
			client:     client,
			request:    &crawlrpc.OrderAck{LeaseId: leaseID, Requeue: requeue},
			policy:     policy,
		}.settle()
	}
}

func (s leaseSettlementSession) settle() error {
	if s.serviceCtx.Err() == nil {
		err := s.retry(s.serviceCtx)
		if err == nil || s.serviceCtx.Err() == nil {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(
		context.WithoutCancel(s.serviceCtx),
		s.policy.shutdownWait,
	)
	defer cancel()

	return s.retry(shutdownCtx)
}

func (s leaseSettlementSession) retry(ctx context.Context) error {
	retryWait := s.policy.retryWait
	retries := 0
	for {
		callCtx, cancel := context.WithTimeout(ctx, orderAckTimeout)
		_, err := s.client.AckOrder(callCtx, s.request)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("settle crawl order lease: %w", ctx.Err())
		}
		if !retryableLeaseSettlementStatus(status.Code(err)) {
			return fmt.Errorf("settle crawl order lease: %w", err)
		}
		retries++
		if retries == 1 {
			slog.WarnContext(
				ctx,
				msgLeaseSettlementRetry,
				slog.String("leaseId", s.request.GetLeaseId()),
				slog.Bool("requeue", s.request.GetRequeue()),
				slog.Any("error", err),
			)
		}
		if !waitForLeaseSettlementRetry(
			ctx,
			jitteredLeaseSettlementRetryWait(retryWait, cryptorand.Reader),
		) {
			return fmt.Errorf("settle crawl order lease: %w", ctx.Err())
		}
		retryWait = min(s.policy.maximumRetryWait, retryWait*2)
	}
}

func retryableLeaseSettlementStatus(code codes.Code) bool {
	switch code {
	case codes.Canceled,
		codes.DeadlineExceeded,
		codes.Unknown,
		codes.ResourceExhausted,
		codes.Aborted,
		codes.Internal,
		codes.Unavailable:
		return true
	default:
		return false
	}
}

func waitForLeaseSettlementRetry(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		timer.Stop()

		return false
	}
}

func jitteredLeaseSettlementRetryWait(wait time.Duration, entropy io.Reader) time.Duration {
	half := wait / 2
	offset, err := cryptorand.Int(entropy, big.NewInt(int64(wait-half)))
	if err != nil {
		return half
	}

	return half + time.Duration(offset.Int64())
}
