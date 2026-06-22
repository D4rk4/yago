package boltvault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	bolt "go.etcd.io/bbolt"
)

var lengthBucket = []byte("__lengths__")

var (
	errVaultClosed     = errors.New("vault closed")
	errDuplicateBucket = errors.New("bucket already registered")
	errReadOnly        = errors.New("write inside read-only transaction")
)

type Vault struct {
	db         *bolt.DB
	quotaBytes int64
	mu         sync.Mutex
	registered map[Name]struct{}
}

func Open(path string, quotaBytes int64) (*Vault, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}

	if err := db.Update(func(tx *bolt.Tx) error {
		if _, createErr := tx.CreateBucketIfNotExists(lengthBucket); createErr != nil {
			return fmt.Errorf("create length bucket: %w", createErr)
		}

		return nil
	}); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("initialize storage: %w: %w", err, closeErr)
		}

		return nil, fmt.Errorf("initialize storage: %w", err)
	}

	return &Vault{db: db, quotaBytes: quotaBytes, registered: map[Name]struct{}{}}, nil
}

func (v *Vault) Close() error {
	if v == nil || v.db == nil {
		return nil
	}

	err := v.db.Close()
	v.db = nil
	if err != nil {
		return fmt.Errorf("close storage: %w", err)
	}

	return nil
}
