package crawlbroker

import (
	"context"
	"encoding/hex"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) ReportProgress(
	ctx context.Context,
	report *crawlrpc.CrawlProgressReport,
) (*crawlrpc.CrawlProgressAck, error) {
	authorization := leaseAuthorization{
		LeaseID:         report.GetLeaseId(),
		WorkerID:        report.GetWorkerId(),
		WorkerSessionID: report.GetWorkerSessionId(),
		RunID:           hex.EncodeToString(report.GetRunId()),
	}
	if authorization.LeaseID == "" ||
		!validCrawlerLeaseIdentity(
			authorization.WorkerID,
			authorization.WorkerSessionID,
		) || len(report.GetRunId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty crawl progress lease identity")
	}
	if !s.sessions.current(authorization.WorkerID, authorization.WorkerSessionID) {
		return nil, status.Error(codes.FailedPrecondition, errLeaseLost.Error())
	}
	release, err := s.queue.beginAuthorizedLeaseMutation(ctx, authorization)
	if err != nil {
		if errors.Is(err, errLeaseLost) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}

		return nil, status.Error(codes.Internal, err.Error())
	}
	defer release()
	progress := progressFromReport(report)
	if err := s.recordAuthorizedProgress(ctx, progress); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &crawlrpc.CrawlProgressAck{}, nil
}
