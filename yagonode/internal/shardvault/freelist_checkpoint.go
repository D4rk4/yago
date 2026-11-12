package shardvault

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	vaultFreelistCheckpointStartedMessage  = "vault freelist checkpoint started"
	vaultFreelistCheckpointFinishedMessage = "vault freelist checkpoint finished"
	vaultFreelistCheckpointFailedMessage   = "vault freelist checkpoint failed"
)

type freelistCheckpointOperations struct {
	checkpoint func(*bolt.DB) error
	sync       func(*bolt.DB) error
}

var checkpointLiveShardFreelist = checkpointShardFreelist

func (e *engine) CheckpointFreelists(ctx context.Context) (vault.FreelistCheckpointResult, error) {
	if err := ctx.Err(); err != nil {
		return vault.FreelistCheckpointResult{}, err
	}
	e.globalGate.Lock()
	defer e.globalGate.Unlock()
	if err := ctx.Err(); err != nil {
		return vault.FreelistCheckpointResult{}, err
	}

	result := vault.FreelistCheckpointResult{ShardsTotal: len(e.shards)}
	slog.DebugContext(ctx, vaultFreelistCheckpointStartedMessage,
		slog.Int("shards", result.ShardsTotal),
	)
	err := checkpointOpenShardFreelists(
		e.shards,
		e.deferFsync.Load(),
		freelistCheckpointOperations{checkpoint: checkpointLiveShardFreelist, sync: syncDB},
		&result,
	)
	if err != nil {
		slog.WarnContext(ctx, vaultFreelistCheckpointFailedMessage,
			slog.Int("checkpointedShards", result.ShardsCheckpointed),
			slog.Int("totalShards", result.ShardsTotal),
			slog.Any("error", err),
		)

		return result, err
	}
	slog.DebugContext(ctx, vaultFreelistCheckpointFinishedMessage,
		slog.Int("checkpointedShards", result.ShardsCheckpointed),
		slog.Int("totalShards", result.ShardsTotal),
	)

	return result, nil
}

func checkpointOpenShardFreelists(
	shards []*bolt.DB,
	deferredFsync bool,
	operations freelistCheckpointOperations,
	result *vault.FreelistCheckpointResult,
) error {
	for _, database := range shards {
		database.NoSync = false
		database.NoFreelistSync = false
	}
	defer func() {
		for _, database := range shards {
			database.NoSync = deferredFsync
		}
	}()

	failures := make([]error, 0)
	for shard, database := range shards {
		if err := operations.checkpoint(database); err != nil {
			failures = append(failures, fmt.Errorf("checkpoint shard %d: %w", shard+1, err))
			if deferredFsync {
				if syncErr := operations.sync(database); syncErr != nil {
					failures = append(
						failures,
						fmt.Errorf("sync shard %d after checkpoint failure: %w", shard+1, syncErr),
					)
				}
			}
			continue
		}
		result.ShardsCheckpointed++
	}

	return errors.Join(failures...)
}
