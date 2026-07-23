package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

func openTestFrontier(t *testing.T) *recrawlfrontier.Frontier {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open recrawl storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	frontier, err := recrawlfrontier.Open(storage)
	if err != nil {
		t.Fatalf("open recrawl frontier: %v", err)
	}

	return frontier
}
