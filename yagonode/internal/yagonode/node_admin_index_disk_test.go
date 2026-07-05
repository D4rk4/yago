package yagonode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func indexDirWithBytes(t *testing.T, size int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "segment"), make([]byte, size), 0o600); err != nil {
		t.Fatalf("write index file: %v", err)
	}

	return dir
}

func TestIndexSourceReportsDiskAndVaultUsage(t *testing.T) {
	v, err := memvault.Open(4 << 20)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	source := newIndexSource(stubSearchIndex{
		stats: searchindex.IndexStats{Documents: 2, Backend: "bleve"},
	}).withDisk(indexDirWithBytes(t, 2048), v)

	got := source.Index(context.Background())

	if got.DiskSize != "2.0 KiB" {
		t.Fatalf("disk size = %q, want 2.0 KiB", got.DiskSize)
	}
	if got.VaultUsed == "" {
		t.Fatalf("vault used missing: %+v", got)
	}
	if got.VaultQuota != "4.0 MiB" {
		t.Fatalf("vault quota = %q, want 4.0 MiB", got.VaultQuota)
	}
}

func TestIndexDiskUsageDegradesGracefully(t *testing.T) {
	// Missing index directory and nil vault leave all rows hidden.
	var view adminui.IndexStats
	indexDiskUsage{indexPath: "/nonexistent/index/dir"}.fill(context.Background(), &view)
	if view.DiskSize != "" || view.VaultUsed != "" || view.VaultQuota != "" {
		t.Fatalf("degraded view = %+v, want empty", view)
	}

	// An unlimited (zero) quota reads as such; a cancelled context hides usage.
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	view = adminui.IndexStats{}
	indexDiskUsage{vault: v}.fill(cancelled, &view)
	if view.VaultUsed != "" {
		t.Fatalf("vault used with cancelled context = %q, want hidden", view.VaultUsed)
	}
	if view.VaultQuota != "unlimited" {
		t.Fatalf("vault quota = %q, want unlimited", view.VaultQuota)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                 "0 B",
		512:               "512 B",
		2048:              "2.0 KiB",
		3 << 20:           "3.0 MiB",
		int64(5) << 30:    "5.0 GiB",
		int64(1536) << 30: "1.5 TiB",
		int64(2) << 50:    "2.0 PiB",
	}
	for input, want := range cases {
		if got := humanBytes(input); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", input, got, want)
		}
	}
}
