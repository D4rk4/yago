package frontier

import (
	"context"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

type WaitingPage struct {
	Page        Page
	Reason      string
	AvailableAt time.Time
}

func (frontier *Frontier) WaitingSample(
	ctx context.Context,
	runIDs []string,
	maximumPerRun int,
	maximumTotal int,
	now time.Time,
) ([]WaitingPage, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if maximumPerRun < 1 || maximumTotal < 1 {
		return nil, fmt.Errorf("waiting page sample limits must be positive")
	}
	waiting := make([]WaitingPage, 0, min(maximumTotal, len(runIDs)*maximumPerRun))
	err := frontier.database.View(func(transaction *bolt.Tx) error {
		readyRuns := transaction.Bucket([]byte(readyBucketName))
		pages := transaction.Bucket([]byte(pagesBucketName))
		hosts := transaction.Bucket([]byte(hostsBucketName))
		for _, runID := range runIDs {
			if err := checkContext(ctx); err != nil {
				return err
			}
			if runID == "" {
				return fmt.Errorf("crawl run identity is required")
			}
			if len(waiting) == maximumTotal {
				break
			}
			ready := readyRuns.Bucket([]byte(runID))
			if ready == nil {
				continue
			}
			cursor := ready.Cursor()
			key, pageID := cursor.First()
			for sampled := 0; sampled < maximumPerRun && len(waiting) < maximumTotal; sampled++ {
				if key == nil {
					break
				}
				if len(key) != 16 {
					return fmt.Errorf("invalid frontier ready key")
				}
				page, err := loadPage(pages.Get(pageID))
				if err != nil {
					return err
				}
				if page.RunID != runID || page.State != StateQueued && page.State != StateDeferred {
					return fmt.Errorf("invalid waiting frontier page %s", page.ID)
				}
				due := timeFromReadyKey(key)
				schedule, err := loadHostSchedule(hosts.Get([]byte(page.Host)))
				if err != nil {
					return err
				}
				waiting = append(waiting, waitingPage(page, due, schedule, now, frontier.config.MaximumHostActive))
				key, pageID = cursor.Next()
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sample waiting frontier pages: %w", err)
	}

	return waiting, nil
}

func waitingPage(page Page, due time.Time, schedule hostSchedule, now time.Time, maximumHostActive int) WaitingPage {
	waiting := WaitingPage{Page: page}
	switch {
	case schedule.Active >= maximumHostActive:
		waiting.Reason = "host concurrency"
	case due.After(now) && due.After(schedule.NextAllowed):
		waiting.Reason = "retry delay"
		waiting.AvailableAt = due
	case schedule.NextAllowed.After(now):
		waiting.Reason = "host politeness"
		waiting.AvailableAt = schedule.NextAllowed
	case due.After(now):
		waiting.Reason = "retry delay"
		waiting.AvailableAt = due
	default:
		waiting.Reason = "queued"
		waiting.AvailableAt = now
	}

	return waiting
}
