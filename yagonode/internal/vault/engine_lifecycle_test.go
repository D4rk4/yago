package vault

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleUsageEngine struct {
	*scriptedEngine
	started   chan struct{}
	release   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	quota     atomic.Int64
}

func (e *lifecycleUsageEngine) UsedBytes(ctx context.Context) (int64, error) {
	e.startOnce.Do(func() { close(e.started) })
	select {
	case <-ctx.Done():
		return 0, fmt.Errorf("wait for lifecycle usage release: %w", ctx.Err())
	case <-e.release:
		return 11, nil
	}
}

func (e *lifecycleUsageEngine) QuotaBytes() int64 {
	return e.quota.Load()
}

func (e *lifecycleUsageEngine) SetQuotaBytes(quota int64) {
	e.quota.Store(quota)
}

func (e *lifecycleUsageEngine) Close() error {
	close(e.closed)

	return nil
}

func TestVaultCloseWaitsForCapacityOperations(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(context.Context, *Vault) error
	}{
		{
			name: "exact usage",
			run: func(ctx context.Context, storage *Vault) error {
				_, err := storage.UsedBytes(ctx)

				return err
			},
		},
		{
			name: "capacity observation",
			run: func(ctx context.Context, storage *Vault) error {
				_, err := storage.AtCapacity(ctx)

				return err
			},
		},
	} {
		t.Run(operation.name, func(t *testing.T) {
			assertVaultCloseWaitsForCapacityOperation(t, operation.run)
		})
	}
}

func assertVaultCloseWaitsForCapacityOperation(
	t *testing.T,
	run func(context.Context, *Vault) error,
) {
	t.Helper()
	engine := &lifecycleUsageEngine{
		scriptedEngine: &scriptedEngine{},
		started:        make(chan struct{}),
		release:        make(chan struct{}),
		closed:         make(chan struct{}),
	}
	engine.quota.Store(10)
	storage, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	operationDone := make(chan error, 1)
	go func() { operationDone <- run(t.Context(), storage) }()
	<-engine.started
	closeStarted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() {
		close(closeStarted)
		closeDone <- storage.Close()
	}()
	<-closeStarted
	waitForVaultCloseWriter(t, storage)
	select {
	case <-engine.closed:
		t.Fatal("engine closed during the capacity operation")
	default:
	}
	quotaDone := make(chan struct{})
	go func() {
		storage.SetQuota(5)
		close(quotaDone)
	}()
	close(engine.release)
	if err := <-operationDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	<-quotaDone
	if _, err := storage.UsedBytes(t.Context()); !errors.Is(err, errVaultClosed) {
		t.Fatalf("post-close usage error = %v", err)
	}
}

func waitForVaultCloseWriter(t *testing.T, storage *Vault) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for storage.lifecycle.TryRLock() {
		storage.lifecycle.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("close did not wait for the capacity operation")
		}
		runtime.Gosched()
	}
}
