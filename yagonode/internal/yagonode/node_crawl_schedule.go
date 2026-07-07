package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlschedule"
)

// Recurring crawls — YaCy's Automation_p parity (UI-19): the schedule loop
// re-dispatches due schedules through the very path the console's crawl-start
// form uses, so scheduled crawls obey the same validation, profiles, and
// format settings as manual ones.

// scheduleCheckInterval is how often the loop looks for due schedules; a
// minute is far below the hour-floor on schedule intervals.
const scheduleCheckInterval = time.Minute

// crawlScheduleSource adapts the schedule store to the admin console.
type crawlScheduleSource struct {
	store    *crawlschedule.Store
	dispatch adminui.CrawlSource
}

// newCrawlScheduleSource wires the console source; nil parts disable it.
func newCrawlScheduleSource(
	store *crawlschedule.Store,
	dispatch adminui.CrawlSource,
) adminui.CrawlScheduleSource {
	if store == nil || dispatch == nil {
		return nil
	}

	return crawlScheduleSource{store: store, dispatch: dispatch}
}

func (s crawlScheduleSource) Schedules(ctx context.Context) []adminui.CrawlScheduleView {
	schedules, err := s.store.List(ctx)
	if err != nil {
		slog.WarnContext(ctx, "list crawl schedules failed", slog.Any("error", err))

		return nil
	}
	views := make([]adminui.CrawlScheduleView, 0, len(schedules))
	for _, schedule := range schedules {
		views = append(views, adminui.CrawlScheduleView{
			ID:       schedule.ID,
			Name:     schedule.Name,
			Seeds:    len(schedule.Seeds),
			Scope:    schedule.Scope,
			Interval: schedule.Interval.String(),
			LastRun:  formatScheduleRun(schedule.LastRun),
			Enabled:  schedule.Enabled,
		})
	}

	return views
}

func (s crawlScheduleSource) CreateSchedule(
	ctx context.Context,
	req adminui.CrawlScheduleRequest,
) error {
	interval, err := time.ParseDuration(req.Interval)
	if err != nil {
		return fmt.Errorf("parse interval: %w", err)
	}
	if _, err := s.store.Create(ctx, crawlschedule.Schedule{
		Name:     req.Name,
		Seeds:    req.Seeds,
		Scope:    req.Scope,
		MaxDepth: req.MaxDepth,
		Interval: interval,
	}); err != nil {
		return fmt.Errorf("create schedule: %w", err)
	}

	return nil
}

func (s crawlScheduleSource) DeleteSchedule(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}

	return nil
}

func (s crawlScheduleSource) SetScheduleEnabled(
	ctx context.Context,
	id string,
	enabled bool,
) error {
	if err := s.store.SetEnabled(ctx, id, enabled); err != nil {
		return fmt.Errorf("toggle schedule: %w", err)
	}

	return nil
}

func formatScheduleRun(at time.Time) string {
	if at.IsZero() {
		return "never"
	}

	return at.UTC().Format("2006-01-02 15:04")
}

// runCrawlScheduleLoop dispatches due schedules until the context ends.
func runCrawlScheduleLoop(
	ctx context.Context,
	store *crawlschedule.Store,
	dispatch adminui.CrawlSource,
) {
	if store == nil || dispatch == nil {
		return
	}
	ticker := time.NewTicker(scheduleCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatchDueSchedules(ctx, store, dispatch)
		}
	}
}

// dispatchDueSchedules starts every due schedule through the console path.
// The schedule is marked as ran even when the dispatcher rejects it (for
// example a duplicate still running): retrying every minute would hammer the
// dispatcher; the next interval retries naturally.
func dispatchDueSchedules(
	ctx context.Context,
	store *crawlschedule.Store,
	dispatch adminui.CrawlSource,
) {
	due, err := store.DueSchedules(ctx)
	if err != nil {
		slog.WarnContext(ctx, "list due crawl schedules failed", slog.Any("error", err))

		return
	}
	for _, schedule := range due {
		outcome, err := dispatch.Start(ctx, adminui.CrawlStart{
			Name:     schedule.Name,
			Seeds:    schedule.Seeds,
			Scope:    schedule.Scope,
			MaxDepth: schedule.MaxDepth,
		})
		if err != nil {
			slog.WarnContext(ctx, "scheduled crawl dispatch failed",
				slog.String("schedule", schedule.ID), slog.Any("error", err))
		} else {
			slog.InfoContext(ctx, "scheduled crawl dispatched",
				slog.String("schedule", schedule.ID),
				slog.String("profileHandle", outcome.ProfileHandle),
				slog.Int("seeds", outcome.Seeds))
		}
		if err := store.MarkRan(ctx, schedule.ID, time.Now()); err != nil {
			slog.WarnContext(ctx, "mark schedule ran failed",
				slog.String("schedule", schedule.ID), slog.Any("error", err))
		}
	}
}

// adminCrawlDispatch resolves the same console dispatch source the schedule
// loop reuses; nil when the node runs without a crawl broker.
func adminCrawlDispatch(assembled node) adminui.CrawlSource {
	dispatcher := crawlDispatcher(assembled.crawl)
	if dispatcher == nil {
		return nil
	}

	return newCrawlSource(dispatcher)
}
