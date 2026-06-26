// Package memvault is the in-memory implementation of the vault Engine. It keeps
// every bucket in process memory, owns no file, and survives only as long as the
// process. It backs tests and any deployment that cannot reach a filesystem.
package memvault

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type engine struct {
	buckets    map[vault.Name]map[string][]byte
	quotaBytes int64
}

func Open(quotaBytes int64) (*vault.Vault, error) {
	vaulted, err := vault.New(&engine{
		buckets:    map[vault.Name]map[string][]byte{},
		quotaBytes: quotaBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize storage: %w", err)
	}

	return vaulted, nil
}

func (e *engine) Provision(name vault.Name) error {
	if _, ok := e.buckets[name]; !ok {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *engine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	staged := snapshot(e.buckets)
	if err := fn(memTxn{buckets: staged, writable: true}); err != nil {
		return err
	}
	e.buckets = staged

	return nil
}

func (e *engine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	return fn(memTxn{buckets: e.buckets, writable: false})
}

func (e *engine) Close() error {
	e.buckets = nil

	return nil
}

func (e *engine) QuotaBytes() int64 {
	return e.quotaBytes
}

func (e *engine) UsedBytes(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("context: %w", err)
	}

	var used int64
	for _, bucket := range e.buckets {
		for key, value := range bucket {
			used += int64(len(key) + len(value))
		}
	}

	return used, nil
}
