package infrastructure

import (
	"context"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

var errStorageClosed = errors.New("storage closed")

func (s *BboltStorage) view(fn func(*bolt.Tx) error) error {
	if s == nil || s.db == nil {
		return wrapStorageError("read storage", errStorageClosed)
	}

	err := s.db.View(fn)
	if err != nil {
		return wrapStorageError("read storage", err)
	}

	return nil
}

func (s *BboltStorage) update(fn func(*bolt.Tx) error) error {
	if s == nil || s.db == nil {
		return wrapStorageError("write storage", errStorageClosed)
	}

	err := s.db.Update(fn)
	if err != nil {
		return wrapStorageError("write storage", err)
	}

	return nil
}

func wrapContextErr(err error) error {
	return fmt.Errorf("context: %w", err)
}

func wrapStorageError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if storageAtCapacityError(err) {
		return fmt.Errorf("%s: %w", operation, ports.ErrAtCapacity)
	}

	return fmt.Errorf("%s: %w", operation, ports.ErrStoreFailure)
}
