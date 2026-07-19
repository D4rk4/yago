package yagonode

import "github.com/D4rk4/yago/yagonode/internal/events"

type nodeEventRecorder interface {
	Record(events.Severity, events.Category, string, string)
}
