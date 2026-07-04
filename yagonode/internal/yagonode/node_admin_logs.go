package yagonode

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

const adminLogLimit = 100

type logsSource struct {
	recorder *events.Recorder
}

func newLogsSource(recorder *events.Recorder) logsSource {
	return logsSource{recorder: recorder}
}

func (s logsSource) Logs(context.Context) []adminui.LogEntry {
	if s.recorder == nil {
		return nil
	}

	recent := s.recorder.Recent(adminLogLimit)
	entries := make([]adminui.LogEntry, 0, len(recent))
	for _, event := range recent {
		entries = append(entries, adminui.LogEntry{
			Time:     event.Time.UTC().Format(time.RFC3339),
			Severity: string(event.Severity),
			Category: string(event.Category),
			Name:     event.Name,
			Message:  event.Message,
		})
	}

	return entries
}
