package boltvault

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	bolt "go.etcd.io/bbolt"
)

var ErrAtCapacity = errors.New("vault at capacity")

func (v *Vault) QuotaBytes() int64 {
	return v.quotaBytes
}

func (v *Vault) UsedBytes(ctx context.Context) (int64, error) {
	if v == nil || v.db == nil {
		return 0, errVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("context: %w", err)
	}

	return v.measureUsedBytes()
}

func (v *Vault) rejectAtCapacity() error {
	if v.quotaBytes <= 0 {
		return nil
	}

	used, err := v.measureUsedBytes()
	if err != nil {
		return err
	}
	if used >= v.quotaBytes {
		return ErrAtCapacity
	}

	return nil
}

func (v *Vault) measureUsedBytes() (int64, error) {
	var used int64
	if err := v.db.View(func(tx *bolt.Tx) error {
		stats := v.db.Stats()
		pageSize := int64(v.db.Info().PageSize)
		free := int64(stats.FreePageN+stats.PendingPageN) * pageSize
		used = tx.Size() - free

		return nil
	}); err != nil {
		return 0, fmt.Errorf("measure used bytes: %w", err)
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
