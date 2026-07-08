package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestCompactorSourceMapsResult(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	source := newCompactorSource(v)
	if source == nil {
		t.Fatal("newCompactorSource returned nil for a real vault")
	}
	result, err := source.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// The in-memory engine has no files to compact, so the adapter maps a zero
	// result with a formatted byte figure.
	if result.ShardsCompacted != 0 || result.BytesReclaimed != "0 B" {
		t.Fatalf("result = %+v, want {0, \"0 B\"}", result)
	}
}

func TestNewCompactorSourceNilVault(t *testing.T) {
	if newCompactorSource(nil) != nil {
		t.Fatal("newCompactorSource(nil) must return a nil Compactor")
	}
}
