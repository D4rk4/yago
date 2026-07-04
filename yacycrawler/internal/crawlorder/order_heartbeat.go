package crawlorder

import (
	"context"
	"log/slog"
	"time"
)

const msgOrderHeartbeatFailed = "crawl order heartbeat failed"

type orderHeartbeat struct {
	stop chan struct{}
	done chan struct{}
}

func keepOrderAlive(
	ctx context.Context,
	delivery CrawlOrderDelivery,
	interval time.Duration,
) *orderHeartbeat {
	beat := &orderHeartbeat{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(beat.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-beat.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := delivery.InProgress(ctx); err != nil {
					slog.WarnContext(ctx, msgOrderHeartbeatFailed,
						slog.String("handle", delivery.Order.Profile.Handle),
						slog.Any("error", err),
					)
				}
			}
		}
	}()
	return beat
}

func (b *orderHeartbeat) release() {
	close(b.stop)
	<-b.done
}
