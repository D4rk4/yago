package crawlorder

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeartbeatKeepsOrderAliveUntilReleased(t *testing.T) {
	var beats atomic.Int32
	delivery := CrawlOrderDelivery{
		InProgress: func(context.Context) error {
			beats.Add(1)
			return nil
		},
	}

	heartbeat := keepOrderAlive(context.Background(), delivery, 5*time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	heartbeat.release()

	settled := beats.Load()
	if settled == 0 {
		t.Fatal("heartbeat never signalled the order was in progress")
	}

	time.Sleep(20 * time.Millisecond)
	if beats.Load() != settled {
		t.Errorf("heartbeat kept beating after release: %d then %d", settled, beats.Load())
	}
}

func TestHeartbeatStopsWhenContextCancelled(t *testing.T) {
	var beats atomic.Int32
	delivery := CrawlOrderDelivery{
		InProgress: func(context.Context) error {
			beats.Add(1)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	heartbeat := keepOrderAlive(ctx, delivery, time.Hour)
	cancel()
	heartbeat.release()

	if beats.Load() != 0 {
		t.Errorf("heartbeat beat despite an immediately cancelled context: %d", beats.Load())
	}
}
