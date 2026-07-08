package crawltraversal

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

const msgOwnershipLapsed = "crawl order ownership heartbeat failed"

type OwnershipLease struct {
	extend   func(context.Context) error
	interval time.Duration
	clock    crawlcapability.Clock
}

func (l OwnershipLease) Renew(ctx context.Context) {
	for {
		if err := l.clock.Sleep(ctx, l.interval); err != nil {
			return
		}
		if err := l.extend(ctx); err != nil {
			slog.WarnContext(ctx, msgOwnershipLapsed, slog.Any("error", err))
		}
	}
}
