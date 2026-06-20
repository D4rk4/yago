package infrastructure

import (
	"errors"
	"strings"
	"syscall"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

func (s *BboltStorage) rejectAtCapacity() error {
	if s.quotaBytes <= 0 {
		return nil
	}

	used, err := s.measureUsedBytes()
	if err != nil {
		return err
	}
	if used >= s.quotaBytes {
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
