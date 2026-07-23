package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type leaseRequeueBatchMutation struct {
	removed        []leaseRecord
	stagedLeaseIDs []string
	requeued       bool
}

func (q *DurableOrderQueue) requeueLeaseBatchTx(
	tx *vault.Txn,
	keys []vault.Key,
	match func(leaseRecord) bool,
) (leaseRequeueBatchMutation, error) {
	mutation := leaseRequeueBatchMutation{
		removed:        make([]leaseRecord, 0, len(keys)),
		stagedLeaseIDs: make([]string, 0),
	}
	for _, key := range keys {
		_, staged, err := q.discoverySettlements.Get(tx, key)
		if err != nil {
			return leaseRequeueBatchMutation{}, fmt.Errorf(
				"read automatic crawl discovery settlement: %w",
				err,
			)
		}
		if staged {
			mutation.stagedLeaseIDs = append(mutation.stagedLeaseIDs, string(key))

			continue
		}
		record, changed, err := q.requeueLeaseTx(tx, key, match)
		if err != nil {
			return leaseRequeueBatchMutation{}, err
		}
		if changed {
			mutation.removed = append(mutation.removed, record)
			mutation.requeued = true
		}
	}

	return mutation, nil
}
