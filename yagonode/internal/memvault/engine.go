// Package memvault is the in-memory implementation of the vault Engine. It keeps
// every bucket in process memory, owns no file, and survives only as long as the
// process. It backs tests and any deployment that cannot reach a filesystem.
package memvault

import (
	"context"
	"fmt"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type engine struct {
	mu         sync.RWMutex
	buckets    map[vault.Name]map[string][]byte
	quotaBytes int64
}

func Open(quotaBytes int64) (*vault.Vault, error) {
	vaulted, _ := vault.New(&engine{
		buckets:    map[vault.Name]map[string][]byte{},
		quotaBytes: quotaBytes,
	})

	return vaulted, nil
}

func (e *engine) Provision(name vault.Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.buckets[name]; !ok {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *engine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

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

	e.mu.RLock()
	defer e.mu.RUnlock()

	return fn(memTxn{buckets: e.buckets, writable: false})
}

func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

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

	e.mu.RLock()
	defer e.mu.RUnlock()

	var used int64
	for _, bucket := range e.buckets {
		for key, value := range bucket {
			used += int64(len(key) + len(value))
		}
	}

	return used, nil
}
