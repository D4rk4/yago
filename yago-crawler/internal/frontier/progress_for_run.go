package frontier

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (frontier *Frontier) ProgressForRun(
	ctx context.Context,
	runID string,
) (QueueDepth, uint64, error) {
	if err := checkContext(ctx); err != nil {
		return QueueDepth{}, 0, err
	}
	if runID == "" {
		return QueueDepth{}, 0, fmt.Errorf("crawl run identity is required")
	}
	var depth QueueDepth
	var pendingIngest uint64
	err := frontier.database.View(func(transaction *bolt.Tx) error {
		var err error
		depth, err = readRunDepth(transaction.Bucket([]byte(runsBucketName)), runID)
		if err != nil {
			return err
		}
		run := transaction.Bucket([]byte(ingestRunsBucketName)).Bucket([]byte(runID))
		if run == nil {
			return nil
		}
		pendingIngest, err = pendingIngestRunCount(run)
		return err
	})
	if err != nil {
		return QueueDepth{}, 0, fmt.Errorf("read crawl progress snapshot: %w", err)
	}
	return depth, pendingIngest, nil
}
