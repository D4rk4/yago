package vault

import (
	"context"
	"errors"
)

var (
	ErrAtCapacity                   = errors.New("vault at capacity")
	ErrCollectionMutationIncomplete = errors.New("collection mutation incomplete")
)

// ErrContended marks a write aborted by shard contention. The engine retries
// the whole update when the update callback RETURNS
// this error — a callback that swallows it turns a retryable abort into data
// loss (STOR-05), so callbacks must propagate it.
var ErrContended = errors.New("storage contended")

var (
	errVaultClosed     = errors.New("vault closed")
	errDuplicateBucket = errors.New("bucket already registered")
	errReadOnly        = errors.New("write inside read-only transaction")
)

type Engine interface {
	Update(ctx context.Context, fn func(EngineTxn) error) error
	View(ctx context.Context, fn func(EngineTxn) error) error
	Provision(Name) error
	UsedBytes(ctx context.Context) (int64, error)
	QuotaBytes() int64
	Close() error
}

type EngineTxn interface {
	Bucket(Name) EngineBucket
	Writable() bool
}

type EngineBucket interface {
	Get(Key) []byte
	Put(Key, []byte) error
	Delete(Key) error
	Scan(prefix Key, fn func(Key, []byte) (bool, error)) error
}
