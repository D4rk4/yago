package crawlbroker

import (
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

func TestPersistentControlDirectiveReplaysUntilAcknowledged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	registry, err := newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("open control registry: %v", err)
	}
	registry.register("worker")
	if !registry.Enqueue("worker", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlPause,
		RunID: "ab",
	}) {
		t.Fatal("enqueue control directive")
	}
	first := deliveredControls(t, registry, "worker")
	if len(first) != 1 || first[0].DirectiveID == 0 {
		t.Fatalf("first delivery = %+v, want one identified directive", first)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	registry, err = newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("reopen control registry: %v", err)
	}
	replayed := deliveredControls(t, registry, "worker")
	if len(replayed) != 1 || replayed[0] != first[0] {
		t.Fatalf("replayed directive = %+v, want %+v", replayed, first)
	}
	if retained := deliveredControls(
		t,
		registry,
		"other",
		first[0].DirectiveID,
	); len(
		retained,
	) != 0 {
		t.Fatalf("other worker delivery = %+v, want empty", retained)
	}
	if remaining := deliveredControls(
		t,
		registry,
		"worker",
		first[0].DirectiveID,
	); len(
		remaining,
	) != 0 {
		t.Fatalf("acknowledged delivery = %+v, want empty", remaining)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close second storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen acknowledged storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry, err = newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("reopen acknowledged registry: %v", err)
	}
	if remaining := deliveredControls(t, registry, "worker"); len(remaining) != 0 {
		t.Fatalf("persisted acknowledged delivery = %+v, want empty", remaining)
	}
}
