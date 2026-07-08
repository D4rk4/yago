package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
)

// themeEventSink adapts the node event recorder for the portal theme store; a
// nil recorder (bare test assemblies) degrades to a no-op sink so theme writes
// never have to care.
func themeEventSink(recorder *events.Recorder) portaltheme.EventSink {
	if recorder == nil {
		return noopThemeEvents{}
	}

	return recorder
}

type noopThemeEvents struct{}

func (noopThemeEvents) Record(events.Severity, events.Category, string, string) {}
