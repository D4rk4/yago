package boltvault

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	bolt "go.etcd.io/bbolt"
)

func (e *engine) QuotaBytes() int64 {
	return e.quotaBytes
}

func (e *engine) UsedBytes(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("context: %w", err)
	}

	var used int64
	if err := e.db.View(func(tx *bolt.Tx) error {
		stats := e.db.Stats()
		pageSize := int64(e.db.Info().PageSize)
		free := int64(stats.FreePageN+stats.PendingPageN) * pageSize
		used = tx.Size() - free

		return nil
	}); err != nil {
		return 0, fmt.Errorf("read storage stats: %w", err)
	}
	if used < 0 {
		used = 0
	}

	return used, nil
}

func storageAtCapacityError(err error) bool {
	if errors.Is(err, syscall.ENOSPC) ||
		errors.Is(err, syscall.EDQUOT) ||
		errors.Is(err, syscall.EFBIG) {
		return true
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "no space left on device") ||
		strings.Contains(message, "disk quota exceeded") ||
		strings.Contains(message, "file too large") ||
		strings.Contains(message, "not enough space")
}
