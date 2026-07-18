package frontiercheckpoint

import (
	"context"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (checkpoint *FrontierCheckpoint) RebindWorkerSession(
	ctx context.Context,
	expected crawlsettlement.Settlement,
	replacementWorkerSessionID string,
) (crawlsettlement.Settlement, bool, error) {
	if !crawlsettlement.Validate(expected) ||
		expected.Phase != crawlsettlement.AwaitingAcknowledgment ||
		!yagocrawlcontract.ValidCrawlerSessionIdentity(replacementWorkerSessionID) {
		return crawlsettlement.Settlement{}, false, crawlsettlement.ErrDefinitionConflict
	}
	var result workerSessionRebindingResult
	err := checkpoint.writeTransaction(
		ctx,
		rebindTerminalSettlementWorkerSession(expected, replacementWorkerSessionID, &result),
	)

	return result.settlement, result.found, err
}

type workerSessionRebindingResult struct {
	settlement crawlsettlement.Settlement
	found      bool
}

func rebindTerminalSettlementWorkerSession(
	expected crawlsettlement.Settlement,
	replacementWorkerSessionID string,
	result *workerSessionRebindingResult,
) func(*bolt.Tx) error {
	return func(transaction *bolt.Tx) error {
		*result = workerSessionRebindingResult{}
		outbox, current, exists, err := readTerminalSettlement(
			transaction,
			expected.LeaseID,
			expected.OrderIdentity,
		)
		if err != nil || !exists {
			return err
		}
		current, err = rebindAwaitingTerminalSettlement(
			current,
			expected,
			replacementWorkerSessionID,
		)
		if err != nil {
			return err
		}
		if err := writeTerminalSettlement(outbox, current); err != nil {
			return err
		}
		result.settlement = crawlsettlement.Clone(current)
		result.found = true

		return nil
	}
}

func rebindAwaitingTerminalSettlement(
	current crawlsettlement.Settlement,
	expected crawlsettlement.Settlement,
	replacementWorkerSessionID string,
) (crawlsettlement.Settlement, error) {
	if current.Phase != crawlsettlement.AwaitingAcknowledgment ||
		len(current.ConfirmationToken) != 0 ||
		!crawlsettlement.SameDefinitionExceptWorkerSession(current, expected) ||
		!yagocrawlcontract.ValidCrawlerSessionIdentity(replacementWorkerSessionID) {
		return crawlsettlement.Settlement{}, crawlsettlement.ErrDefinitionConflict
	}
	current.WorkerSessionID = replacementWorkerSessionID

	return current, nil
}
