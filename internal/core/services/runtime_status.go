package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

type RuntimeStatus struct {
	identity Identity
	clock    ports.Clock
	rwi      ports.RWIStore
	urls     ports.URLStore
	version  string
	start    time.Time
}

func NewRuntimeStatus(
	identity Identity,
	clock ports.Clock,
	rwi ports.RWIStore,
	urls ports.URLStore,
	version string,
) RuntimeStatus {
	return RuntimeStatus{
		identity: identity,
		clock:    clock,
		rwi:      rwi,
		urls:     urls,
		version:  version,
		start:    clock.Now(),
	}
}

func (s RuntimeStatus) Snapshot(ctx context.Context) contracts.StatusSnapshot {
	now := s.clock.Now()
	uptime := int(now.Sub(s.start).Minutes())

	counts := seedCounts{
		rwi: s.count(ctx, s.rwi.RWICount),
		url: s.count(ctx, s.urls.URLCount),
	}

	return contracts.StatusSnapshot{
		Version: s.version,
		Uptime:  uptime,
		Seed:    s.identity.assembleSeed(now, uptime, s.version, counts),
	}
}

func (s RuntimeStatus) count(ctx context.Context, fn func(context.Context) (int, error)) int {
	n, err := fn(ctx)
	if err != nil {
		slog.WarnContext(ctx, "count unavailable for status snapshot", "error", err)

		return 0
	}

	return n
}
