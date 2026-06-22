package nodestatus

import (
	"context"
	"time"
)

type Liveness struct {
	version string
	start   time.Time
	now     func() time.Time
}

func NewLiveness(version string) Liveness {
	return Liveness{version: version, start: time.Now(), now: time.Now}
}

func (l Liveness) Version(context.Context) string {
	return l.version
}

func (l Liveness) Uptime(context.Context) int {
	return l.uptimeMinutes(l.now())
}

func (l Liveness) uptimeMinutes(now time.Time) int {
	return int(now.Sub(l.start).Minutes())
}
