package frontier

import (
	"context"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

func (frontier *Frontier) CancelRun(ctx context.Context, runID string) (int, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	if runID == "" {
		return 0, fmt.Errorf("crawl run identity is required")
	}
	cancelled := 0
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		pages := transaction.Bucket([]byte(pagesBucketName))
		metadata := transaction.Bucket([]byte(metadataBucketName))
		runs := transaction.Bucket([]byte(runsBucketName))
		runDepth, err := readRunDepth(runs, runID)
		if err != nil {
			return err
		}
		depth, err := readQueueDepth(metadata)
		if err != nil {
			return err
		}
		pendingPages := make(map[string]Page)
		pageCursor := pages.Cursor()
		for _, raw := pageCursor.First(); raw != nil; _, raw = pageCursor.Next() {
			if err := checkContext(ctx); err != nil {
				return err
			}
			page, err := loadPage(raw)
			if err != nil {
				return err
			}
			if page.RunID == runID && (page.State == StateQueued || page.State == StateDeferred) {
				pendingPages[page.ID] = page
			}
		}
		ready := transaction.Bucket([]byte(readyBucketName)).Bucket([]byte(runID))
		if len(pendingPages) > 0 && ready == nil {
			return fmt.Errorf("crawl run ready queue is missing")
		}
		if ready != nil {
			cursor := ready.Cursor()
			for _, pageID := cursor.First(); pageID != nil; _, pageID = cursor.Next() {
				if _, found := pendingPages[string(pageID)]; !found {
					continue
				}
				if err := cursor.Delete(); err != nil {
					return err
				}
				delete(pendingPages, string(pageID))
			}
		}
		if len(pendingPages) > 0 {
			return fmt.Errorf("crawl run ready queue is missing %d pages", len(pendingPages))
		}
		cursor := pages.Cursor()
		for _, raw := cursor.First(); raw != nil; _, raw = cursor.Next() {
			page, err := loadPage(raw)
			if err != nil {
				return err
			}
			if page.RunID != runID || (page.State != StateQueued && page.State != StateDeferred) {
				continue
			}
			if page.State == StateQueued {
				if depth.Queued == 0 || runDepth.Queued == 0 {
					return fmt.Errorf("crawl run queued depth underflow")
				}
				depth.Queued--
				runDepth.Queued--
			} else {
				if depth.Deferred == 0 || runDepth.Deferred == 0 {
					return fmt.Errorf("crawl run deferred depth underflow")
				}
				depth.Deferred--
				runDepth.Deferred--
			}
			page.State = StateCancelled
			page.LeaseUntil = time.Time{}
			if err := savePage(pages, page); err != nil {
				return err
			}
			depth.Cancelled++
			runDepth.Cancelled++
			cancelled++
		}
		if err := writeQueueState(metadata, StateQueued, depth.Queued); err != nil {
			return err
		}
		if err := writeQueueState(metadata, StateDeferred, depth.Deferred); err != nil {
			return err
		}
		if err := writeQueueState(metadata, StateCancelled, depth.Cancelled); err != nil {
			return err
		}
		return writeRunDepth(runs, runID, runDepth)
	})
	if err != nil {
		return 0, fmt.Errorf("cancel crawl run frontier: %w", err)
	}
	return cancelled, nil
}
