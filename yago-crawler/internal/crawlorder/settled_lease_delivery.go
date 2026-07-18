package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
)

func settleGrantedLease(
	settlement func(context.Context) error,
	leaseID string,
	grants *crawllease.GrantRegistry,
) func(context.Context) error {
	return func(ctx context.Context) error {
		protected := grants != nil && grants.BeginSettlement(leaseID)
		if err := settlement(ctx); err != nil {
			if protected {
				grants.SettlementFailed(leaseID)
			}

			return err
		}
		if grants != nil {
			grants.Settle(leaseID)
		}

		return nil
	}
}
