package crawlbroker

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (s *exchangeServer) recordAuthorizedProgress(
	ctx context.Context,
	progress yagocrawlcontract.CrawlRunProgress,
) error {
	if progress.State == yagocrawlcontract.CrawlRunRunning {
		if err := s.control.reassignAuthorizedRun(
			ctx,
			progress.WorkerID,
			progress.RunID,
		); err != nil {
			return err
		}
	}
	s.progress.Record(ctx, progress)

	return nil
}
