package crawlbroker

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (s *exchangeServer) recordCurrentProgress(
	ctx context.Context,
	progress yagocrawlcontract.CrawlRunProgress,
) error {
	if progress.State == yagocrawlcontract.CrawlRunRunning {
		ownership, err := s.control.ReassignRunIfLeaseOwned(
			ctx,
			s.queue,
			progress.WorkerID,
			progress.RunID,
		)
		if err != nil || ownership != runLeaseOwnedByWorker {
			return err
		}
		s.progress.Record(ctx, progress)

		return nil
	}
	owned, err := s.queue.terminalProgressOwnedBy(
		ctx,
		progress.WorkerID,
		progress.RunID,
	)
	if err != nil || !owned {
		return err
	}
	s.progress.Record(ctx, progress)

	return nil
}
