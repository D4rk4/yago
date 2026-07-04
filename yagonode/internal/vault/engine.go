package vault

import (
	"context"
	"errors"
)

var ErrAtCapacity = errors.New("vault at capacity")

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
