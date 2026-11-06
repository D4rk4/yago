package crawlbroker

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) acknowledgeUnrecordedNotModified(
	ctx context.Context,
	authorization leaseAuthorization,
) (*crawlrpc.IngestAck, error) {
	if !s.sessions.current(authorization.WorkerID, authorization.WorkerSessionID) {
		return nil, status.Error(codes.FailedPrecondition, errLeaseLost.Error())
	}
	if err := s.queue.verifyLeaseAuthorization(ctx, authorization); err != nil {
		if errors.Is(err, errLeaseLost) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}

		return nil, status.Error(codes.Unavailable, err.Error())
	}

	return &crawlrpc.IngestAck{}, nil
}
