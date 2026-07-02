// Package boltvault is the bbolt implementation of the vault Engine. It owns the
// embedded database file and is the single holder of the database handle; no
// caller receives the raw handle and no bolt type appears on its exported
// surface.
package boltvault

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type engine struct {
	db         *bolt.DB
	quotaBytes int64
}

var newVault = vault.New

func Open(path string, quotaBytes int64) (*vault.Vault, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}

	vaulted, err := newVault(&engine{db: db, quotaBytes: quotaBytes})
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("initialize storage: %w", err)
	}

	return vaulted, nil
}

func (e *engine) Provision(name vault.Name) error {
	if err := e.db.Update(func(tx *bolt.Tx) error {
		if _, createErr := tx.CreateBucketIfNotExists([]byte(name)); createErr != nil {
			return fmt.Errorf("create bucket: %w", createErr)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("provision bucket %s: %w", name, err)
	}

	return nil
}

func (e *engine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	if err := e.db.Update(func(tx *bolt.Tx) error {
		return fn(boltTxn{tx: tx, writable: true})
	}); err != nil {
		if storageAtCapacityError(err) {
			return vault.ErrAtCapacity
		}

		return fmt.Errorf("update storage: %w", err)
	}

	return nil
}

func (e *engine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	if err := e.db.View(func(tx *bolt.Tx) error {
		return fn(boltTxn{tx: tx, writable: false})
	}); err != nil {
		return fmt.Errorf("read storage: %w", err)
	}

	return nil
}

func (e *engine) Close() error {
	return wrapCloseError(e.db.Close())
}

func wrapCloseError(err error) error {
	if err != nil {
		return fmt.Errorf("close storage: %w", err)
	}

	return nil
}

type boltTxn struct {
	tx       *bolt.Tx
	writable bool
}

func (t boltTxn) Writable() bool { return t.writable }

func (t boltTxn) Bucket(name vault.Name) vault.EngineBucket {
	return boltBucket{bucket: t.tx.Bucket([]byte(name))}
}

type boltBucket struct {
	bucket *bolt.Bucket
}

func (b boltBucket) Get(key vault.Key) []byte {
	return b.bucket.Get(key)
}

func (b boltBucket) Put(key vault.Key, val []byte) error {
	if err := b.bucket.Put(key, val); err != nil {
		return fmt.Errorf("store: %w", err)
	}

	return nil
}

func (b boltBucket) Delete(key vault.Key) error {
	if err := b.bucket.Delete(key); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	return nil
}

func (b boltBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	cursor := b.bucket.Cursor()

	var key, raw []byte
	if len(prefix) == 0 {
		key, raw = cursor.First()
	} else {
		key, raw = cursor.Seek(prefix)
	}

	for ; key != nil; key, raw = cursor.Next() {
		if len(prefix) > 0 && !bytes.HasPrefix(key, prefix) {
			break
		}

		keep, err := fn(key, raw)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}
