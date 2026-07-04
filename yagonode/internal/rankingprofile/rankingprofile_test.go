package rankingprofile

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestHolderDefaultsWhenUnset(t *testing.T) {
	vault, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })

	holder, err := Open(context.Background(), vault)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if holder.Current() != searchindex.DefaultRankingWeights() {
		t.Fatalf("current = %+v, want default", holder.Current())
	}
}

func TestHolderNilCurrentIsDefault(t *testing.T) {
	var holder *Holder
	if holder.Current() != searchindex.DefaultRankingWeights() {
		t.Fatalf("nil current = %+v, want default", holder.Current())
	}
}

func TestHolderSetPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	vault, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault: %v", err)
	}

	holder, err := Open(context.Background(), vault)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	want := searchindex.RankingWeights{Title: 9, Headings: 2, Anchors: 2, Body: 1, URL: 3}
	if err := holder.Set(context.Background(), want); err != nil {
		t.Fatalf("set: %v", err)
	}
	if holder.Current() != want {
		t.Fatalf("current after set = %+v, want %+v", holder.Current(), want)
	}
	if err := vault.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	restored, err := Open(context.Background(), reopened)
	if err != nil {
		t.Fatalf("reopen holder: %v", err)
	}
	if restored.Current() != want {
		t.Fatalf("current after reopen = %+v, want %+v", restored.Current(), want)
	}
}

func TestHolderSetRejectsInvalid(t *testing.T) {
	vault, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })

	holder, err := Open(context.Background(), vault)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := holder.Set(context.Background(), searchindex.RankingWeights{}); err == nil {
		t.Fatal("expected validation error for zero weights")
	}
	if holder.Current() != searchindex.DefaultRankingWeights() {
		t.Fatalf("current changed on invalid set: %+v", holder.Current())
	}
}
