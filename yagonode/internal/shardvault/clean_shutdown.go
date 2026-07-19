package shardvault

import (
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

type shardShutdownOperations struct {
	checkpoint func(*bolt.DB) error
	sync       func(*bolt.DB) error
	close      func(*bolt.DB) error
}

func (e *engine) Close() error {
	e.globalGate.Lock()
	defer e.globalGate.Unlock()

	return closeShardDatabases(
		e.shards,
		e.deferFsync.Load(),
		shardShutdownOperations{
			checkpoint: checkpointShardFreelist,
			sync:       syncDB,
			close:      closeDB,
		},
	)
}

func closeShardDatabases(
	shards []*bolt.DB,
	deferredFsync bool,
	operations shardShutdownOperations,
) error {
	failures := make([]error, 0)
	for shard, database := range shards {
		database.NoSync = false
		database.NoFreelistSync = false
		if err := operations.checkpoint(database); err != nil {
			failures = append(failures, fmt.Errorf("checkpoint shard %d: %w", shard, err))
			if deferredFsync {
				if syncErr := operations.sync(database); syncErr != nil {
					failures = append(
						failures,
						fmt.Errorf("sync shard %d after checkpoint failure: %w", shard, syncErr),
					)
				}
			}
		}
		if err := wrapCloseError(operations.close(database)); err != nil {
			failures = append(failures, fmt.Errorf("shard %d: %w", shard, err))
		}
	}

	return errors.Join(failures...)
}

func checkpointShardFreelist(database *bolt.DB) error {
	if err := database.Update(func(*bolt.Tx) error { return nil }); err != nil {
		return fmt.Errorf("persist freelist: %w", err)
	}

	return nil
}

func wrapCloseError(err error) error {
	if err != nil {
		return fmt.Errorf("close storage: %w", err)
	}

	return nil
}
