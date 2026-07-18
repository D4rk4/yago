package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlerCheckpointSession struct {
	checkpoint      *frontiercheckpoint.FrontierCheckpoint
	workerID        string
	workerSessionID string
	leaseGrants     *crawllease.GrantRegistry
}

func openCrawlerCheckpointSession(
	ctx context.Context,
	cfg ServiceConfig,
) (crawlerCheckpointSession, error) {
	checkpoint, err := frontiercheckpoint.Open(
		filepath.Join(cfg.DataDir, "crawler", "frontier-v1.db"),
	)
	if err != nil {
		return crawlerCheckpointSession{}, fmt.Errorf(
			"open crawler frontier checkpoint: %w",
			err,
		)
	}
	workerID, err := checkpoint.WorkerID(cfg.WorkerID)
	if err != nil {
		_ = checkpoint.Close()

		return crawlerCheckpointSession{}, fmt.Errorf(
			"load crawler worker identity: %w",
			err,
		)
	}
	return crawlerCheckpointSession{
		checkpoint:      checkpoint,
		workerID:        workerID,
		workerSessionID: newCrawlerSessionID(workerID),
		leaseGrants: crawllease.NewGrantRegistry(
			ctx,
			yagocrawlcontract.MaximumHeartbeatActiveLeases,
		),
	}, nil
}
