package crawlbroker

import (
	"context"
	"encoding/hex"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

var errIngestDeferred = errors.New("ingest pipeline saturated")

func (s *exchangeServer) SubmitIngest(
	ctx context.Context,
	msg *crawlrpc.IngestBatchMessage,
) (*crawlrpc.IngestAck, error) {
	workerID := msg.GetWorkerId()
	workerSessionID := msg.GetWorkerSessionId()
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) {
		return nil, status.Error(codes.InvalidArgument, "invalid worker session identity")
	}
	batch, err := yagocrawlcontract.UnmarshalIngestBatch(msg.GetBatchJson())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode ingest batch: %v", err)
	}
	authorization := leaseAuthorization{
		LeaseID:         msg.GetLeaseId(),
		WorkerID:        workerID,
		WorkerSessionID: workerSessionID,
		RunID:           hex.EncodeToString(batch.Provenance),
	}
	if authorization.LeaseID == "" || len(batch.Provenance) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty crawl ingest lease identity")
	}
	finish := func() {}
	if s.beginIngest != nil {
		finish = s.beginIngest()
	}

	result := make(chan error, 1)
	delivery := crawlresults.IngestDelivery{
		Batch: batch,
		Ack:   func(context.Context) error { finish(); result <- nil; return nil },
		Nak:   func(context.Context) error { finish(); result <- errIngestDeferred; return nil },
		ValidateMutation: func(context.Context) error {
			if !s.sessions.current(authorization.WorkerID, authorization.WorkerSessionID) {
				return errLeaseLost
			}

			return nil
		},
		BeginMutationGroup: func(mutationContext context.Context) (context.Context, func()) {
			return s.queue.beginAuthorizedLeaseMutationGroup(mutationContext)
		},
		BeginMutation: func(mutationContext context.Context) (func(), error) {
			if !s.queue.authorizedLeaseMutationGroup(mutationContext) &&
				!s.sessions.current(authorization.WorkerID, authorization.WorkerSessionID) {
				return nil, errLeaseLost
			}

			return s.queue.beginAuthorizedLeaseMutation(mutationContext, authorization)
		},
		LeaseLost: func(context.Context) error {
			finish()
			result <- errLeaseLost

			return nil
		},
	}
	select {
	case s.ingest <- delivery:
	case <-ctx.Done():
		finish()
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	select {
	case absorbErr := <-result:
		if absorbErr != nil {
			if errors.Is(absorbErr, errLeaseLost) {
				return nil, status.Error(codes.FailedPrecondition, absorbErr.Error())
			}
			return nil, status.Error(codes.Unavailable, absorbErr.Error())
		}

		return &crawlrpc.IngestAck{}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}
