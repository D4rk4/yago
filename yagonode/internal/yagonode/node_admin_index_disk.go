package yagonode

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// indexDiskUsage measures how much disk the search backends occupy: the size of
// the full-text index directory and the data vault's used bytes against its
// configured quota (YAGO_STORAGE_QUOTA), so the Index section can show current
// size and maximum side by side.
type indexDiskUsage struct {
	indexPath string
	vault     *vault.Vault
}

// withDisk attaches the on-disk locations to the index source. Empty path or nil
// vault simply leave the corresponding rows hidden.
func (s indexSource) withDisk(indexPath string, v *vault.Vault) indexSource {
	s.disk = indexDiskUsage{indexPath: indexPath, vault: v}

	return s
}

func (d indexDiskUsage) fill(ctx context.Context, view *adminui.IndexStats) {
	if d.indexPath != "" {
		if size, ok := dirSizeBytes(d.indexPath); ok {
			view.DiskSize = humanBytes(size)
		}
	}
	if d.vault == nil {
		return
	}
	if used, err := d.vault.UsedBytes(ctx); err == nil {
		view.VaultUsed = humanBytes(used)
	}
	if quota := d.vault.QuotaBytes(); quota > 0 {
		view.VaultQuota = humanBytes(quota)
	} else {
		view.VaultQuota = "unlimited"
	}
}

// dirSizeBytes sums the file sizes under root, reporting false when the tree
// cannot be walked (missing directory, permissions).
func dirSizeBytes(root string) (int64, bool) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}

		return nil
	})

	return total, err == nil
}

const bytesPerUnit = 1024

// humanBytes renders a byte count in binary units with one decimal (12.3 MiB).
func humanBytes(n int64) string {
	if n < bytesPerUnit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	unit := 0
	for value >= bytesPerUnit*bytesPerUnit && unit < len(units)-1 {
		value /= bytesPerUnit
		unit++
	}

	return fmt.Sprintf("%.1f %s", value/bytesPerUnit, units[unit])
}
