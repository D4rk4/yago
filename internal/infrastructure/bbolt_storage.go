package infrastructure

import (
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

type BboltStorage struct {
	db         *bolt.DB
	quotaBytes int64
}

func OpenBboltStorage(path string, quotaBytes int64) (*BboltStorage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}

	store := &BboltStorage{db: db, quotaBytes: quotaBytes}
	if err := store.ensureBuckets(); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("initialize storage: %w: %w", err, closeErr)
		}

		return nil, fmt.Errorf("initialize storage: %w", err)
	}

	return store, nil
}

func (s *BboltStorage) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.db = nil
	if err != nil {
		return fmt.Errorf("close storage: %w", err)
	}

	return nil
}

var (
	_ ports.RWIStore = (*BboltStorage)(nil)
	_ ports.URLStore = (*BboltStorage)(nil)
)
