package frontier

import (
	"context"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

func (frontier *Frontier) Settle(
	ctx context.Context,
	pageID string,
	state State,
	notBefore time.Time,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if pageID == "" || !validState(state) || state == StateFetching {
		return errors.New("invalid page settlement")
	}
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		pages := transaction.Bucket([]byte(pagesBucketName))
		page, err := loadPage(pages.Get([]byte(pageID)))
		if err != nil {
			return err
		}
		if page.State != StateFetching {
			return fmt.Errorf("page %s is not fetching", page.ID)
		}
		metadata := transaction.Bucket([]byte(metadataBucketName))
		runs := transaction.Bucket([]byte(runsBucketName))
		depth, err := readQueueDepth(metadata)
		if err != nil {
			return err
		}
		if depth.Fetching == 0 {
			return fmt.Errorf("frontier fetching count underflow")
		}
		runDepth, err := readRunDepth(runs, page.RunID)
		if err != nil {
			return err
		}
		if runDepth.Fetching == 0 {
			return fmt.Errorf("crawl run fetching depth underflow")
		}
		runDepth.Fetching--
		if err := writeQueueState(metadata, StateFetching, depth.Fetching-1); err != nil {
			return err
		}
		hosts := transaction.Bucket([]byte(hostsBucketName))
		schedule, err := loadHostSchedule(hosts.Get([]byte(page.Host)))
		if err != nil {
			return err
		}
		if schedule.Active == 0 {
			return fmt.Errorf("host %s active count underflow", page.Host)
		}
		schedule.Active--
		if err := saveHostSchedule(hosts, page.Host, schedule); err != nil {
			return err
		}
		page.State = state
		page.LeaseUntil = time.Time{}
		page.NotBefore = notBefore.UTC()
		if state == StateQueued || state == StateDeferred {
			if notBefore.IsZero() {
				page.NotBefore = time.Now().UTC()
			}
			ready, err := transaction.Bucket([]byte(readyBucketName)).CreateBucketIfNotExists([]byte(page.RunID))
			if err != nil {
				return err
			}
			sequence, err := ready.NextSequence()
			if err != nil {
				return err
			}
			if err := ready.Put(
				readyKey(page.NotBefore, sequence), []byte(page.ID),
			); err != nil {
				return err
			}
			if state == StateQueued {
				runDepth.Queued++
				if err := writeQueueState(metadata, StateQueued, depth.Queued+1); err != nil {
					return err
				}
			} else {
				runDepth.Deferred++
				if err := writeQueueState(metadata, StateDeferred, depth.Deferred+1); err != nil {
					return err
				}
			}
		} else {
			if err := writeQueueState(metadata, state, depth.For(state)+1); err != nil {
				return err
			}
			runDepth.Add(state)
		}
		if err := writeRunDepth(runs, page.RunID, runDepth); err != nil {
			return err
		}
		return savePage(pages, page)
	})
	if err != nil {
		return fmt.Errorf("settle frontier page: %w", err)
	}
	return nil
}

func (frontier *Frontier) RecoverExpired(ctx context.Context, now time.Time) (int, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	recovered := 0
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		pages := transaction.Bucket([]byte(pagesBucketName))
		hosts := transaction.Bucket([]byte(hostsBucketName))
		readyRoot := transaction.Bucket([]byte(readyBucketName))
		metadata := transaction.Bucket([]byte(metadataBucketName))
		runs := transaction.Bucket([]byte(runsBucketName))
		depth, err := readQueueDepth(metadata)
		if err != nil {
			return err
		}
		cursor := pages.Cursor()
		for _, raw := cursor.First(); raw != nil; _, raw = cursor.Next() {
			if err := checkContext(ctx); err != nil {
				return err
			}
			page, err := loadPage(raw)
			if err != nil {
				return err
			}
			if page.State != StateFetching || page.LeaseUntil.After(now) {
				continue
			}
			page.State = StateQueued
			page.LeaseUntil = time.Time{}
			page.NotBefore = now.UTC()
			ready, err := readyRoot.CreateBucketIfNotExists([]byte(page.RunID))
			if err != nil {
				return err
			}
			sequence, err := ready.NextSequence()
			if err != nil {
				return err
			}
			if err := ready.Put(readyKey(page.NotBefore, sequence), []byte(page.ID)); err != nil {
				return err
			}
			schedule, err := loadHostSchedule(hosts.Get([]byte(page.Host)))
			if err != nil {
				return err
			}
			if schedule.Active == 0 {
				return fmt.Errorf("host %s active count underflow", page.Host)
			}
			schedule.Active--
			if err := saveHostSchedule(hosts, page.Host, schedule); err != nil {
				return err
			}
			if err := savePage(pages, page); err != nil {
				return err
			}
			runDepth, err := readRunDepth(runs, page.RunID)
			if err != nil {
				return err
			}
			if runDepth.Fetching == 0 {
				return fmt.Errorf("crawl run fetching depth underflow")
			}
			runDepth.Fetching--
			runDepth.Queued++
			if err := writeRunDepth(runs, page.RunID, runDepth); err != nil {
				return err
			}
			depth.Fetching--
			depth.Queued++
			recovered++
		}
		if err := writeQueueState(metadata, StateFetching, depth.Fetching); err != nil {
			return err
		}
		return writeQueueState(metadata, StateQueued, depth.Queued)
	})
	if err != nil {
		return 0, fmt.Errorf("recover expired frontier leases: %w", err)
	}
	return recovered, nil
}
