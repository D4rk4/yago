package recrawlfrontier

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type RecrawlSchedule struct {
	URL   string
	DueAt time.Time
}

func (f *Frontier) ScheduleRecrawlsAtIfAbsent(
	ctx context.Context,
	profileHandle string,
	schedules []RecrawlSchedule,
	retryInterval time.Duration,
) error {
	if len(schedules) == 0 {
		return nil
	}
	if profileHandle == "" || retryInterval <= 0 {
		return fmt.Errorf("schedule imported recrawls: invalid profile or retry interval")
	}
	for _, schedule := range schedules {
		if schedule.URL == "" || schedule.DueAt.IsZero() {
			return fmt.Errorf("schedule imported recrawls: invalid URL or due time")
		}
	}
	if err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, found, err := f.profiles.Get(tx, vault.Key(profileHandle)); err != nil {
			return fmt.Errorf("read recrawl profile: %w", err)
		} else if !found {
			return fmt.Errorf("unknown profile")
		}
		for _, schedule := range schedules {
			hash, _ := yagomodel.HashURL(schedule.URL)
			key := vault.Key(string(hash))
			if _, found, err := f.records.Get(tx, key); err != nil {
				return fmt.Errorf("read existing imported schedule: %w", err)
			} else if found {
				continue
			}
			if err := f.schedule(tx, key, scheduleRecord{
				URL:           schedule.URL,
				ProfileHandle: profileHandle,
				Interval:      retryInterval,
				NextDueAt:     schedule.DueAt.UTC(),
			}); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("schedule imported recrawls: %w", err)
	}

	return nil
}
