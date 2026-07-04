package urldenylist

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// TestSplitKeyWithoutSeparator covers the defensive fallback for a stored key
// that lacks the kind/value separator — unreachable through the public API, which
// always writes both, but guarded so a corrupt key cannot panic a scan.
func TestSplitKeyWithoutSeparator(t *testing.T) {
	kind, value := splitKey(vault.Key("garbage"))
	if kind != Kind("garbage") || value != "" {
		t.Fatalf("splitKey(garbage) = %q, %q; want (garbage, \"\")", kind, value)
	}
}
