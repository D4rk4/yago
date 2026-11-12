package yagonode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlStateAutomaticMaximumUsesQuotaAndReservedFreeSpace(t *testing.T) {
	quota := int64(160 << 30)
	available := uint64(200 << 30)
	reserve := uint64(1 << 30)
	snapshot := yagocrawlcontract.StoragePressureSnapshot{
		Policy:               yagocrawlcontract.StoragePressurePolicy{ReservedFreeBytes: reserve},
		AvailableBytes:       available,
		MeasurementAvailable: true,
	}
	if got, want := crawlStateAutomaticMaximum(quota, snapshot), int64(16<<30); got != want {
		t.Fatalf("quota-derived maximum = %d, want %d", got, want)
	}
	snapshot.AvailableBytes = uint64(9 << 30)
	if got, want := crawlStateAutomaticMaximum(quota, snapshot), int64(8<<30); got != want {
		t.Fatalf("free-space-limited maximum = %d, want %d", got, want)
	}
	snapshot.AvailableBytes = uint64(200 << 30)
	if got, want := crawlStateAutomaticMaximum(
		0,
		snapshot,
	), int64(
		(uint64(199)<<30)/10,
	); got != want {
		t.Fatalf("unlimited-quota maximum = %d, want %d", got, want)
	}
	snapshot.AvailableBytes = reserve
	if got := crawlStateAutomaticMaximum(quota, snapshot); got != 0 {
		t.Fatalf("maximum with no space above reserve = %d, want 0", got)
	}
	snapshot.MeasurementAvailable = false
	if got, want := crawlStateAutomaticMaximum(
		quota,
		snapshot,
	), quota/crawlerNodeStateQuotaDivisor; got != want {
		t.Fatalf("maximum without measurement = %d, want quota-derived %d", got, want)
	}
}

func TestCrawlStateAutomaticMaximumMeasuresStoragePressure(t *testing.T) {
	pressure := yagocrawlcontract.NewStoragePressureGate(
		t.TempDir(),
		yagocrawlcontract.StoragePressurePolicy{},
	)
	if got, want := crawlStateAutomaticMaximumBytes(100, pressure), int64(10); got != want {
		t.Fatalf("measured automatic maximum = %d, want %d", got, want)
	}
}

func TestCrawlStateAutomaticMaximumTracksLiveQuotaAndExplicitOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crawlbroker.db")
	if err := os.WriteFile(path, []byte("12345678901"), 0o600); err != nil {
		t.Fatalf("write crawl state: %v", err)
	}
	admission := newCrawlStateGrowthAdmission(path, 0, nil)
	admission.SetMaximumPolicy(crawlStateMaximumPolicy{Automatic: true})
	admission.SetStorageQuota(100)
	if err := admission.CheckGrowth(); !errors.Is(err, errCrawlStateMaximum) {
		t.Fatalf("automatic quota maximum admitted oversized state: %v", err)
	}
	admission.SetStorageQuota(200)
	admission.SetStorageQuota(-1)
	if got, want := admission.CrawlStateMaximumBytes(), int64(20); got != want {
		t.Fatalf("negative quota changed automatic state maximum to %d, want %d", got, want)
	}
	if err := admission.CheckGrowth(); err != nil {
		t.Fatalf("increased live quota did not increase automatic maximum: %v", err)
	}
	admission.SetMaximumPolicy(crawlStateMaximumPolicy{MaximumBytes: 10})
	if err := admission.CheckGrowth(); !errors.Is(err, errCrawlStateMaximum) {
		t.Fatalf("explicit maximum did not override live quota: %v", err)
	}
	admission.SetMaximumBytes(0)
	if err := admission.CheckGrowth(); err != nil {
		t.Fatalf("explicit zero did not disable maximum: %v", err)
	}
}
