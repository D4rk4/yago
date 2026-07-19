package yagonode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestCrawlStateMaximumRejectsFreshOrdersButPreservesDuplicatesAndIngest(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	if err := os.WriteFile(path, []byte("full"), 0o600); err != nil {
		t.Fatalf("write full crawl state marker: %v", err)
	}
	admission := newCrawlStateGrowthAdmission(path, 4, nil)
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open crawl broker storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	broker, err := crawlbroker.Open(
		crawlbroker.Config{ListenAddr: "127.0.0.1:0", GrowthAdmission: admission},
		storage,
		nil,
	)
	if err != nil {
		t.Fatalf("open crawl broker: %v", err)
	}
	t.Cleanup(broker.Close)
	order := crawlStateTestOrder("state-maximum")
	if _, err := broker.Orders.PublishOnce(
		t.Context(),
		"fresh",
		order,
	); !errors.Is(
		err,
		errCrawlStateMaximum,
	) {
		t.Fatalf("fresh order error = %v, want %v", err, errCrawlStateMaximum)
	}
	admission.SetMaximumBytes(0)
	duplicate, err := broker.Orders.PublishOnce(t.Context(), "accepted", order)
	if err != nil || duplicate {
		t.Fatalf("accepted order duplicate=%t error=%v", duplicate, err)
	}
	admission.SetMaximumBytes(4)
	duplicate, err = broker.Orders.PublishOnce(t.Context(), "accepted", order)
	if err != nil || !duplicate {
		t.Fatalf("duplicate under state maximum duplicate=%t error=%v", duplicate, err)
	}
	if ingestAdmission := crawlStateLifecycleAdmission(admission); ingestAdmission != nil {
		t.Fatalf("state maximum leaked into ingest admission: %T", ingestAdmission)
	}
}

func TestCrawlStateMaximumPreservesFilesystemPressureForIngestAndRecovery(t *testing.T) {
	upstream := &nodeGrowthAdmission{err: yagocrawlcontract.ErrStorageHeadroom}
	admission := newCrawlStateGrowthAdmission(
		filepath.Join(t.TempDir(), crawlBrokerStateFileName),
		1,
		upstream,
	)
	if crawlStateLifecycleAdmission(admission) != upstream {
		t.Fatal("ingest and recovery did not retain filesystem pressure admission")
	}
	if err := crawlStateLifecycleAdmission(
		admission,
	).CheckGrowth(); !errors.Is(
		err,
		yagocrawlcontract.ErrStorageHeadroom,
	) {
		t.Fatalf("lifecycle admission error = %v", err)
	}
	if err := admission.CheckGrowth(); !errors.Is(err, yagocrawlcontract.ErrStorageHeadroom) {
		t.Fatalf("fresh-growth admission error = %v", err)
	}
}

func TestCrawlStateMaximumHandlesMissingAvailableAndUnreadableState(t *testing.T) {
	directory := t.TempDir()
	missing := newCrawlStateGrowthAdmission(filepath.Join(directory, "missing.db"), 1, nil)
	if err := missing.CheckGrowth(); err != nil {
		t.Fatalf("missing crawl state admission: %v", err)
	}
	availablePath := filepath.Join(directory, "available.db")
	if err := os.WriteFile(availablePath, []byte("room"), 0o600); err != nil {
		t.Fatalf("write available crawl state: %v", err)
	}
	available := newCrawlStateGrowthAdmission(availablePath, 5, nil)
	if err := available.CheckGrowth(); err != nil {
		t.Fatalf("available crawl state admission: %v", err)
	}
	loopPath := filepath.Join(directory, "loop.db")
	if err := os.Symlink(loopPath, loopPath); err != nil {
		t.Fatalf("create crawl state symlink loop: %v", err)
	}
	unreadable := newCrawlStateGrowthAdmission(loopPath, 1, nil)
	if err := unreadable.CheckGrowth(); err == nil || errors.Is(err, errCrawlStateMaximum) {
		t.Fatalf("unreadable crawl state admission = %v", err)
	}
}
