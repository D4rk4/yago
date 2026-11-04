package shardvault

import (
	"sync"

	bolt "go.etcd.io/bbolt"
)

func commitShardTransactions(transactions []*bolt.Tx) error {
	written := make([]*bolt.Tx, 0, len(transactions))
	for _, tx := range transactions {
		if tx == nil {
			continue
		}
		if !holdsPendingWrites(tx) {
			_ = tx.Rollback()

			continue
		}
		written = append(written, tx)
	}
	if len(written) == 1 {
		return commitShardTransaction(written[0])
	}
	failures := make([]error, len(written))
	var commits sync.WaitGroup
	for index, tx := range written {
		commits.Go(func() {
			failures[index] = commitShardTransaction(tx)
		})
	}
	commits.Wait()
	for _, failure := range failures {
		if failure != nil {
			return failure
		}
	}

	return nil
}

func commitShardTransaction(tx *bolt.Tx) error {
	if err := commitTx(tx); err != nil {
		_ = tx.Rollback()

		return err
	}

	return nil
}
