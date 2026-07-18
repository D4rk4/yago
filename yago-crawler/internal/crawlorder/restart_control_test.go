package crawlorder

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRestartControlHandlerFiresRestart(t *testing.T) {
	restarts := 0
	next := &recordingControlHandler{}
	handler := NewRestartControlHandler(func() { restarts++ }, next)

	handler.Apply(context.Background(), yagocrawlcontract.CrawlControlDirective{
		Kind: yagocrawlcontract.CrawlControlRestart,
	})

	if restarts != 1 {
		t.Fatalf("restart fired %d times, want 1", restarts)
	}
	if len(next.snapshot()) != 0 {
		t.Fatal("a restart directive must not reach the run-steering handler")
	}
}

func TestRestartControlHandlerDelegatesOtherKinds(t *testing.T) {
	restarts := 0
	next := &recordingControlHandler{}
	handler := NewRestartControlHandler(func() { restarts++ }, next)

	handler.Apply(context.Background(), yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: "ab",
	})

	if restarts != 0 {
		t.Fatal("a non-restart directive must not fire the restart trigger")
	}
	applied := next.snapshot()
	if len(applied) != 1 || applied[0].Kind != yagocrawlcontract.CrawlControlCancel {
		t.Fatalf("delegated = %+v, want one cancel", applied)
	}
}

func TestRestartControlHandlerToleratesNilSeams(t *testing.T) {
	NewRestartControlHandler(nil, nil).Apply(
		context.Background(),
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlRestart},
	)
	NewRestartControlHandler(nil, nil).Apply(
		context.Background(),
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlPause},
	)
}
