package adminui

import "context"

type IndexRebuildScheduler interface {
	RebuildPending(ctx context.Context) (bool, error)
	ScheduleRebuild(ctx context.Context) error
}
