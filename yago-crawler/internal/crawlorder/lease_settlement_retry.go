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

type leasedOrderAcknowledgment struct {
	leaseID         string
	workerID        string
	workerSessionID string
	requeue         bool
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

func settleLeaseForSession(
	serviceCtx context.Context,
	client OrderStreamer,
	acknowledgment leasedOrderAcknowledgment,
) func(context.Context) error {
	return settleOrderAcknowledgmentWithPolicy(
		serviceCtx,
		client,
		&crawlrpc.OrderAck{
			LeaseId:         acknowledgment.leaseID,
			Requeue:         acknowledgment.requeue,
			WorkerId:        acknowledgment.workerID,
			WorkerSessionId: acknowledgment.workerSessionID,
		},
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
	return settleOrderAcknowledgmentWithPolicy(
		serviceCtx,
		client,
		&crawlrpc.OrderAck{LeaseId: leaseID, Requeue: requeue},
		policy,
	)
}

func settleOrderAcknowledgmentWithPolicy(
	serviceCtx context.Context,
	client OrderStreamer,
	request *crawlrpc.OrderAck,
	policy leaseSettlementPolicy,
) func(context.Context) error {
	acknowledge := acknowledgeOrderWithPolicy(serviceCtx, client, request, policy)

	return func(ctx context.Context) error {
		_, err := acknowledge(ctx)

		return err
	}
}

func acknowledgeOrderWithPolicy(
	serviceCtx context.Context,
	client OrderStreamer,
	request *crawlrpc.OrderAck,
	policy leaseSettlementPolicy,
) func(context.Context) (*crawlrpc.OrderAckResult, error) {
	return func(context.Context) (*crawlrpc.OrderAckResult, error) {
		if request.GetLeaseId() == "" {
			return nil, fmt.Errorf("settle crawl order lease: empty lease id")
		}
		return leaseSettlementSession{
			serviceCtx: serviceCtx,
			client:     client,
			request:    request,
			policy:     policy,
		}.settleResult()
	}
}

func (s leaseSettlementSession) settleResult() (*crawlrpc.OrderAckResult, error) {
	if s.serviceCtx.Err() == nil {
		result, err := s.retryResult(s.serviceCtx)
		if err == nil || s.serviceCtx.Err() == nil {
			return result, err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(
		context.WithoutCancel(s.serviceCtx),
		s.policy.shutdownWait,
	)
	defer cancel()

	return s.retryResult(shutdownCtx)
}

func (s leaseSettlementSession) retry(ctx context.Context) error {
	_, err := s.retryResult(ctx)

	return err
}

func (s leaseSettlementSession) retryResult(
	ctx context.Context,
) (*crawlrpc.OrderAckResult, error) {
	retryWait := s.policy.retryWait
	retries := 0
	for {
		callCtx, cancel := context.WithTimeout(ctx, orderAckTimeout)
		result, err := s.client.AckOrder(callCtx, s.request)
		cancel()
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("settle crawl order lease: %w", ctx.Err())
		}
		if !retryableLeaseSettlementStatus(status.Code(err)) {
			return nil, fmt.Errorf("settle crawl order lease: %w", err)
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
			return nil, fmt.Errorf("settle crawl order lease: %w", ctx.Err())
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
