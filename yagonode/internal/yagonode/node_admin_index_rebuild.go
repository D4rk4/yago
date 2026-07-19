package yagonode

import (
	"context"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const (
	indexRebuildScheduledEvent = "index.rebuild.scheduled"
	indexRebuildFailedEvent    = "index.rebuild.schedule_failed"
)

type indexRebuildScheduler struct {
	path     string
	recorder *events.Recorder
}

func newIndexRebuildScheduler(
	path string,
	recorder *events.Recorder,
) adminui.IndexRebuildScheduler {
	if strings.TrimSpace(path) == "" {
		return nil
	}

	return indexRebuildScheduler{path: path, recorder: recorder}
}

func (s indexRebuildScheduler) RebuildPending(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("check rebuild status context: %w", err)
	}
	pending, err := searchindex.DiskRebuildPending(s.path)
	if err != nil {
		return false, fmt.Errorf("read search index rebuild marker: %w", err)
	}

	return pending, nil
}

func (s indexRebuildScheduler) ScheduleRebuild(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("check rebuild scheduling context: %w", err)
	}
	if err := searchindex.ScheduleDiskRebuild(s.path); err != nil {
		if s.recorder != nil {
			s.recorder.Record(
				events.SeverityWarn,
				events.CategoryStorage,
				indexRebuildFailedEvent,
				"search index rebuild scheduling failed",
			)
		}

		return fmt.Errorf("schedule search index rebuild: %w", err)
	}
	if s.recorder != nil {
		s.recorder.Record(
			events.SeverityInfo,
			events.CategoryStorage,
			indexRebuildScheduledEvent,
			"search index rebuild scheduled for the next node restart",
		)
	}

	return nil
}
