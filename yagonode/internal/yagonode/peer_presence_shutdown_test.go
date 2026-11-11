package yagonode

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type heldPeerPresenceAnnouncer struct {
	started chan struct{}
}

func (a heldPeerPresenceAnnouncer) Run(ctx context.Context) {
	close(a.started)
	<-ctx.Done()
}

func (heldPeerPresenceAnnouncer) GreetDiscovered(context.Context, yagomodel.Seed) {}

func TestPeerPresenceShutdownWaitsForAnnouncer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	announcer := heldPeerPresenceAnnouncer{started: make(chan struct{})}
	var background sync.WaitGroup
	startPeerPresenceLoops(ctx, &background, node{announcer: announcer})
	<-announcer.started

	joined := make(chan struct{})
	go func() {
		background.Wait()
		close(joined)
	}()
	select {
	case <-joined:
		t.Fatal("background wait returned while the announcer was active")
	case <-time.After(25 * time.Millisecond):
	}

	cancel()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("background wait did not finish after the announcer stopped")
	}
}
