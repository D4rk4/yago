package shardvault

import (
	"context"
	"log/slog"
	"os"
	"time"
)

const (
	vaultShardOpeningMessage       = "vault shard opening"
	vaultShardOpenedMessage        = "vault shard opened"
	vaultWordFilterBuildingMessage = "vault word filter building"
	vaultWordFilterCompleteMessage = "vault word filter initialization completed"
)

type startupProgress struct {
	logger *slog.Logger
	clock  func() time.Time
}

func runtimeStartupProgress() startupProgress {
	return startupProgress{logger: slog.Default(), clock: time.Now}
}

func (progress startupProgress) shardOpening(shard, total int, path string) time.Time {
	started := progress.clock()
	progress.logger.LogAttrs(
		context.Background(),
		slog.LevelInfo,
		vaultShardOpeningMessage,
		slog.Int("shard", shard+1),
		slog.Int("total", total),
		slog.Int("completed", shard),
		slog.String("path", path),
		slog.Int64("sizeBytes", shardFileSize(path)),
		slog.Duration("duration", 0),
	)

	return started
}

func (progress startupProgress) shardOpened(
	started time.Time,
	shard, total int,
	path string,
) {
	progress.logger.LogAttrs(
		context.Background(),
		slog.LevelInfo,
		vaultShardOpenedMessage,
		slog.Int("shard", shard+1),
		slog.Int("total", total),
		slog.Int("completed", shard+1),
		slog.String("path", path),
		slog.Int64("sizeBytes", shardFileSize(path)),
		slog.Duration("duration", progress.clock().Sub(started)),
	)
}

func (progress startupProgress) wordFilterBuilding(total int) time.Time {
	started := progress.clock()
	progress.logger.LogAttrs(
		context.Background(),
		slog.LevelInfo,
		vaultWordFilterBuildingMessage,
		slog.Int("total", total),
		slog.Int("completed", 0),
		slog.Duration("duration", 0),
	)

	return started
}

func (progress startupProgress) wordFilterInitialized(
	started time.Time,
	total, degraded int,
) {
	level := slog.LevelInfo
	if degraded > 0 {
		level = slog.LevelWarn
	}
	progress.logger.LogAttrs(
		context.Background(),
		level,
		vaultWordFilterCompleteMessage,
		slog.Int("total", total),
		slog.Int("completed", total),
		slog.Int("degradedShards", degraded),
		slog.Duration("duration", progress.clock().Sub(started)),
	)
}

func shardFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}

	return info.Size()
}
