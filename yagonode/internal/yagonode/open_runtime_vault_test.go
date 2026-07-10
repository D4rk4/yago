package yagonode

import (
	"path/filepath"
	"testing"
)

// TestOpenRuntimeVaultOpensWithWordFilter exercises the real runtime vault
// opener (not a test double), so the per-shard word-filter wiring it bakes in
// (PERF-READ-01) is covered end to end.
func TestOpenRuntimeVaultOpensWithWordFilter(t *testing.T) {
	v, err := openRuntimeVault(filepath.Join(t.TempDir(), "storage"), 1<<30)
	if err != nil {
		t.Fatalf("openRuntimeVault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	if v == nil {
		t.Fatal("openRuntimeVault returned a nil vault")
	}
}
