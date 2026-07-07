package main

import (
	"context"
	"errors"
	"testing"
)

func TestRestartControllerTriggerWrapsCleanShutdown(t *testing.T) {
	ctx, restart := newRestartController(context.Background())

	restart.Trigger()
	restart.Trigger() // idempotent

	if ctx.Err() == nil {
		t.Fatal("Trigger must cancel the run context")
	}
	if err := restart.Wrap(nil); !errors.Is(err, errRestartRequested) {
		t.Fatalf("clean shutdown after Trigger = %v, want errRestartRequested", err)
	}
}

func TestRestartControllerWrapPassesThroughWithoutTrigger(t *testing.T) {
	_, restart := newRestartController(context.Background())

	if err := restart.Wrap(nil); err != nil {
		t.Fatalf("clean shutdown without Trigger = %v, want nil", err)
	}
	sentinel := errors.New("failure")
	if err := restart.Wrap(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("failure must pass through, got %v", err)
	}
}
