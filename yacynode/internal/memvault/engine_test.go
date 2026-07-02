package memvault_test

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/vaulttest"
)

func TestConformance(t *testing.T) {
	vaulttest.RunConformance(t, memvault.Open)
}

func TestUsedBytesRejectsCancelledContext(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := v.UsedBytes(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("UsedBytes err = %v, want context.Canceled", err)
	}
}
