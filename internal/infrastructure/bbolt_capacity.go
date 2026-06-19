package infrastructure

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

func (s *BboltStorage) rejectAtCapacity() error {
	if s.quotaBytes <= 0 {
		return nil
	}

	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat storage: %w", err)
	}
	if info.Size() >= s.quotaBytes {
		return ports.ErrAtCapacity
	}

	return nil
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
