package vault

import (
	"context"
	"fmt"
	"sync"
)

const lengthBucket = Name("__lengths__")

type Vault struct {
	engine     Engine
	mu         sync.Mutex
	registered map[Name]struct{}
}

func New(engine Engine) (*Vault, error) {
	if engine == nil {
		return nil, errVaultClosed
	}
	if err := engine.Provision(lengthBucket); err != nil {
		return nil, fmt.Errorf("provision length bucket: %w", err)
	}

	return &Vault{engine: engine, registered: map[Name]struct{}{}}, nil
}

func (v *Vault) Close() error {
	if v == nil || v.engine == nil {
		return nil
	}

	err := v.engine.Close()
	v.engine = nil
	if err != nil {
		return fmt.Errorf("close storage: %w", err)
	}

	return nil
}

func (v *Vault) QuotaBytes() int64 {
	if v == nil || v.engine == nil {
		return 0
	}

	return v.engine.QuotaBytes()
}

func (v *Vault) UsedBytes(ctx context.Context) (int64, error) {
	if v == nil || v.engine == nil {
		return 0, errVaultClosed
	}

	used, err := v.engine.UsedBytes(ctx)
	if err != nil {
		return 0, fmt.Errorf("measure used bytes: %w", err)
	}

	return used, nil
}

// CompactResult reports what a Compact pass reclaimed.
type CompactResult struct {
	ShardsCompacted int
	BytesReclaimed  int64
}

// compactor is the optional engine capability behind Vault.Compact. Only the
// on-disk sharded engine implements it; the in-memory engine has no files to
// reclaim, so Compact is a no-op there.
type compactor interface {
	Compact(ctx context.Context) (CompactResult, error)
}

// Compact asks the engine to return space freed by deletes back to the OS. Live
// usage (UsedBytes) already excludes freed pages, but the files keep their
// high-water size until compacted (ADR-0036 C). It is a no-op on engines that
// do not support compaction.
func (v *Vault) Compact(ctx context.Context) (CompactResult, error) {
	if v == nil || v.engine == nil {
		return CompactResult{}, errVaultClosed
	}
	c, ok := v.engine.(compactor)
	if !ok {
		return CompactResult{}, nil
	}
	result, err := c.Compact(ctx)
	if err != nil {
		return CompactResult{}, fmt.Errorf("compact storage: %w", err)
	}

	return result, nil
}

// shardGrower is the optional engine capability behind Vault.GrowShards. Only the
// sharded engine grows; the in-memory engine is a single store, so it is a no-op.
type shardGrower interface {
	GrowShards(ctx context.Context, maxSplits int) (int, error)
}

// GrowShards asks the engine to split its overfull shards, bounded to maxSplits
// per call. It is a no-op on engines that do not shard (ADR-0037).
func (v *Vault) GrowShards(ctx context.Context, maxSplits int) (int, error) {
	if v == nil || v.engine == nil {
		return 0, errVaultClosed
	}
	grower, ok := v.engine.(shardGrower)
	if !ok {
		return 0, nil
	}
	splits, err := grower.GrowShards(ctx, maxSplits)
	if err != nil {
		return 0, fmt.Errorf("grow shards: %w", err)
	}

	return splits, nil
}

// quotaSetter is the optional engine capability behind Vault.SetQuota. The
// sharded engine carries a mutable ceiling; an engine without one keeps the
// quota it opened with.
type quotaSetter interface {
	SetQuotaBytes(quotaBytes int64)
}

// SetQuota changes the live disk-budget ceiling without reopening the vault.
// AtCapacity and the eviction sweep read QuotaBytes each cycle, so the new
// ceiling takes effect on the next sweep — no restart, no reshard (ADR-0037 D).
// It is a no-op on engines whose quota is fixed at open.
func (v *Vault) SetQuota(quotaBytes int64) {
	if v == nil || v.engine == nil {
		return
	}
	if setter, ok := v.engine.(quotaSetter); ok {
		setter.SetQuotaBytes(quotaBytes)
	}
}

// deferredSyncer is the optional engine capability behind deferred fsync
// (ADR-0038): the sharded engine can run its shards in NoSync mode and flush
// them on a cadence. The in-memory engine has nothing to flush, so it does not
// implement it and the vault methods below are no-ops there.
type deferredSyncer interface {
	SetDeferredFsync(enabled bool)
	SyncShards(ctx context.Context) error
	DeferredFsyncEnabled() bool
}

// SetDeferredFsync switches the engine between per-commit fsync and deferred
// fsync (ADR-0038). The node calls it once at boot with the operator's
// restart-required setting; it is a no-op on engines that always fsync.
func (v *Vault) SetDeferredFsync(enabled bool) {
	if v == nil || v.engine == nil {
		return
	}
	if syncer, ok := v.engine.(deferredSyncer); ok {
		syncer.SetDeferredFsync(enabled)
	}
}

// SyncShards flushes the engine's deferred writes to disk, spreading the fsync
// load across its shards (ADR-0038). It is a no-op — returning nil — on engines
// that always fsync, so the maintenance loop can call it unconditionally.
func (v *Vault) SyncShards(ctx context.Context) error {
	if v == nil || v.engine == nil {
		return errVaultClosed
	}
	syncer, ok := v.engine.(deferredSyncer)
	if !ok {
		return nil
	}
	if err := syncer.SyncShards(ctx); err != nil {
		return fmt.Errorf("sync shards: %w", err)
	}

	return nil
}

// DeferredFsyncEnabled reports whether the engine is deferring fsync, so the
// maintenance loop knows whether its flush pass has work. False on engines that
// always fsync.
func (v *Vault) DeferredFsyncEnabled() bool {
	if v == nil || v.engine == nil {
		return false
	}
	syncer, ok := v.engine.(deferredSyncer)
	if !ok {
		return false
	}

	return syncer.DeferredFsyncEnabled()
}

func (v *Vault) AtCapacity(ctx context.Context) (bool, error) {
	if v == nil || v.engine == nil {
		return false, errVaultClosed
	}
	if v.engine.QuotaBytes() <= 0 {
		return false, nil
	}

	used, err := v.UsedBytes(ctx)
	if err != nil {
		return false, err
	}

	return used >= v.engine.QuotaBytes(), nil
}
