package frontiercheckpoint

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) boundedWriteTransaction(
	ctx context.Context,
	write func(*bolt.Tx) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write frontier checkpoint: %w", err)
	}
	checkpoint.mutex.RLock()
	defer checkpoint.mutex.RUnlock()
	if checkpoint.database == nil {
		return ErrClosed
	}

	return wrapDatabaseError("write frontier checkpoint", checkpoint.database.Update(write))
}
