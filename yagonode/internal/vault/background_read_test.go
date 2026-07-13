package vault

import (
	"context"
	"errors"
	"testing"
)

func TestBackgroundReadPreservesContextLifecycle(t *testing.T) {
	if IsBackgroundRead(t.Context()) {
		t.Fatal("ordinary context marked as background read")
	}
	ctx, cancel := context.WithCancel(t.Context())
	background := BackgroundRead(ctx)
	if !IsBackgroundRead(background) {
		t.Fatal("background read marker missing")
	}
	cancel()
	if !errors.Is(background.Err(), context.Canceled) {
		t.Fatalf("background context error = %v", background.Err())
	}
}
