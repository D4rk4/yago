package frontier

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

type hostSchedule struct {
	Active      int       `json:"active"`
	NextAllowed time.Time `json:"nextAllowed"`
}

func (frontier *Frontier) Claim(
	ctx context.Context,
	runID string,
	now time.Time,
) (Page, bool, error) {
	if err := checkContext(ctx); err != nil {
		return Page{}, false, err
	}
	if runID == "" {
		return Page{}, false, fmt.Errorf("crawl run identity is required")
	}
	var claimed Page
	found := false
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		ready := transaction.Bucket([]byte(readyBucketName)).Bucket([]byte(runID))
		if ready == nil {
			return nil
		}
		pages := transaction.Bucket([]byte(pagesBucketName))
		hosts := transaction.Bucket([]byte(hostsBucketName))
		metadata := transaction.Bucket([]byte(metadataBucketName))
		runs := transaction.Bucket([]byte(runsBucketName))
		cursor := ready.Cursor()
		scanned := 0
		for key, pageID := cursor.First(); key != nil; key, pageID = cursor.Next() {
			if err := checkContext(ctx); err != nil {
				return err
			}
			scanned++
			if scanned > maximumReadyScan {
				return nil
			}
			if len(key) != 16 {
				return fmt.Errorf("invalid frontier ready key")
			}
			due := timeFromReadyKey(key)
			if due.After(now) {
				return nil
			}
			page, err := loadPage(pages.Get(pageID))
			if err != nil {
				return err
			}
			if page.RunID != runID {
				return fmt.Errorf(
					"frontier run index contains page %s from %s",
					page.ID,
					page.RunID,
				)
			}
			if page.State != StateQueued && page.State != StateDeferred {
				return fmt.Errorf("ready page %s is %s", page.ID, page.State)
			}
			schedule, err := loadHostSchedule(hosts.Get([]byte(page.Host)))
			if err != nil {
				return err
			}
			if schedule.Active >= frontier.config.MaximumHostActive ||
				schedule.NextAllowed.After(now) {
				deferUntil := schedule.NextAllowed
				if !deferUntil.After(now) {
					deferUntil = now.Add(frontier.config.HostDelay)
				}
				if !deferUntil.After(now) {
					deferUntil = now.Add(100 * time.Millisecond)
				}
				if err := cursor.Delete(); err != nil {
					return err
				}
				page.NotBefore = deferUntil.UTC()
				if err := savePage(pages, page); err != nil {
					return err
				}
				sequence, err := ready.NextSequence()
				if err != nil {
					return err
				}
				if err := ready.Put(readyKey(page.NotBefore, sequence), pageID); err != nil {
					return err
				}
				continue
			}
			if err := cursor.Delete(); err != nil {
				return err
			}
			previousState := page.State
			page.State = StateFetching
			page.LeaseUntil = now.Add(frontier.config.LeaseDuration).UTC()
			page.Attempts++
			schedule.Active++
			schedule.NextAllowed = now.Add(frontier.config.HostDelay).UTC()
			if err := savePage(pages, page); err != nil {
				return err
			}
			if err := saveHostSchedule(hosts, page.Host, schedule); err != nil {
				return err
			}
			depth, err := readQueueDepth(metadata)
			if err != nil {
				return err
			}
			runDepth, err := readRunDepth(runs, page.RunID)
			if err != nil {
				return err
			}
			if previousState == StateQueued {
				if depth.Queued == 0 {
					return fmt.Errorf("frontier queue count underflow")
				}
				depth.Queued--
				if runDepth.Queued == 0 {
					return fmt.Errorf("crawl run queue depth underflow")
				}
				runDepth.Queued--
			} else {
				if depth.Deferred == 0 {
					return fmt.Errorf("frontier deferred count underflow")
				}
				depth.Deferred--
				if runDepth.Deferred == 0 {
					return fmt.Errorf("crawl run deferred depth underflow")
				}
				runDepth.Deferred--
			}
			runDepth.Fetching++
			if err := writeQueueState(metadata, StateQueued, depth.Queued); err != nil {
				return err
			}
			if err := writeQueueState(metadata, StateDeferred, depth.Deferred); err != nil {
				return err
			}
			if err := writeQueueState(metadata, StateFetching, depth.Fetching+1); err != nil {
				return err
			}
			if err := writeRunDepth(runs, page.RunID, runDepth); err != nil {
				return err
			}
			claimed = page
			found = true
			return nil
		}
		return nil
	})
	if err != nil {
		return Page{}, false, fmt.Errorf("claim frontier page: %w", err)
	}
	return claimed, found, nil
}

func timeFromReadyKey(key []byte) time.Time {
	encoded := uint64(0)
	for index := 0; index < 8; index++ {
		encoded = encoded<<8 | uint64(key[index])
	}
	return time.Unix(0, int64(encoded^(uint64(1)<<63))).UTC()
}

func loadHostSchedule(raw []byte) (hostSchedule, error) {
	if len(raw) == 0 {
		return hostSchedule{}, nil
	}
	var schedule hostSchedule
	if err := json.Unmarshal(raw, &schedule); err != nil {
		return hostSchedule{}, fmt.Errorf("decode host schedule: %w", err)
	}
	if schedule.Active < 0 {
		return hostSchedule{}, fmt.Errorf("invalid host active count")
	}
	return schedule, nil
}

func saveHostSchedule(bucket *bolt.Bucket, host string, schedule hostSchedule) error {
	encoded, err := json.Marshal(schedule)
	if err != nil {
		return fmt.Errorf("encode host schedule: %w", err)
	}
	return bucket.Put([]byte(host), encoded)
}
