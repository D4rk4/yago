package hostrank

import "testing"

func TestHolderServesEmptyTableBeforeFirstStore(t *testing.T) {
	holder := NewHolder()

	got := holder.Current()
	if got == nil || len(got) != 0 {
		t.Fatalf("pre-store table = %v, want empty non-nil", got)
	}
	if got.Rank("anything") != 0 {
		t.Fatalf("pre-store rank = %v, want 0", got.Rank("anything"))
	}
}

func TestHolderStoreThenCurrentReturnsLatest(t *testing.T) {
	holder := NewHolder()

	holder.Store(Table{"a": 0.5})
	if got := holder.Current().Rank("a"); got != 0.5 {
		t.Fatalf("stored rank = %v, want 0.5", got)
	}

	holder.Store(Table{"b": 0.9})
	if got := holder.Current().Rank("a"); got != 0 {
		t.Fatalf("stale host still present after overwrite: %v", got)
	}
	if got := holder.Current().Rank("b"); got != 0.9 {
		t.Fatalf("overwritten rank = %v, want 0.9", got)
	}
}
