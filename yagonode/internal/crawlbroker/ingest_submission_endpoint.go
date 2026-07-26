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
	batchJSON := msg.GetBatchJson()
	if len(batchJSON) > yagocrawlcontract.MaximumIngestBatchBytes {
		return nil, status.Error(codes.InvalidArgument, "ingest batch exceeds size limit")
	}
	batch, err := yagocrawlcontract.UnmarshalIngestBatch(batchJSON)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode ingest batch: %v", err)
	}
	authorization := leaseAuthorization{
		LeaseID:         msg.GetLeaseId(),
		WorkerID:        workerID,
		WorkerSessionID: workerSessionID,
		RunID:           hex.EncodeToString(batch.Provenance),
		ProfileHandle:   batch.ProfileHandle,
	}
	if authorization.LeaseID == "" || len(batch.Provenance) == 0 || batch.ProfileHandle == "" {
		return nil, status.Error(codes.InvalidArgument, "empty crawl ingest lease identity")
	}
	finish := func() {}
	if s.beginIngest != nil {
		finish = s.beginIngest()
	}

	result := make(chan ingestOutcome, 1)
	delivery := s.authorizedIngestDelivery(
		batch,
		len(batchJSON),
		authorization,
		finish,
		result,
	)
	select {
	case s.ingest <- delivery:
	case <-ctx.Done():
		finish()
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	select {
	case outcome := <-result:
		if outcome.err != nil {
			if errors.Is(outcome.err, errLeaseLost) {
				return nil, status.Error(codes.FailedPrecondition, outcome.err.Error())
			}
			return nil, status.Error(codes.Unavailable, outcome.err.Error())
		}

		return &crawlrpc.IngestAck{
			Rejected:      outcome.rejectionRule != "",
			RejectionRule: outcome.rejectionRule,
		}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

// ingestOutcome is how one absorbed delivery ended. A nil error with a
// non-empty rejectionRule means the node consumed the batch but refused to
// store its document, which the crawler must not tally as indexed.
type ingestOutcome struct {
	err           error
	rejectionRule string
}

func (s *exchangeServer) authorizedIngestDelivery(
	batch yagocrawlcontract.IngestBatch,
	batchJSONSize int,
	authorization leaseAuthorization,
	finish func(),
	result chan<- ingestOutcome,
) crawlresults.IngestDelivery {
	authorizedProfile := new(yagocrawlcontract.CrawlProfile)

	return crawlresults.IngestDelivery{
		Batch:         batch,
		CrawlProfile:  authorizedProfile,
		BatchJSONSize: batchJSONSize,
		Ack: func(context.Context) error {
			finish()
			result <- ingestOutcome{}

			return nil
		},
		Reject: func(_ context.Context, rule string) error {
			finish()
			result <- ingestOutcome{rejectionRule: rule}

			return nil
		},
		Nak: func(context.Context) error {
			finish()
			result <- ingestOutcome{err: errIngestDeferred}

			return nil
		},
		AuthorizeLeaseSnapshot: func(mutationContext context.Context) error {
			if !s.sessions.current(authorization.WorkerID, authorization.WorkerSessionID) {
				return errLeaseLost
			}
			profile, err := s.queue.authorizedLeaseProfile(mutationContext, authorization)
			if err != nil {
				return err
			}
			*authorizedProfile = profile

			return nil
		},
		LeaseLost: func(context.Context) error {
			finish()
			result <- ingestOutcome{err: errLeaseLost}

			return nil
		},
	}
}
