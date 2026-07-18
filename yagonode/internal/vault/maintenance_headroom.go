package vault

import (
	"context"
	"fmt"
)

type compactionHeadroomSource interface {
	CompactionHeadroom(context.Context) (uint64, error)
}

type shardGrowthHeadroomSource interface {
	ShardGrowthHeadroom(context.Context) (uint64, error)
}

func (v *Vault) CompactionHeadroom(ctx context.Context) (uint64, error) {
	lease, err := v.acquireEngineLease()
	if err != nil {
		return 0, err
	}
	defer lease.release()
	source, ok := lease.engine.(compactionHeadroomSource)
	if !ok {
		return 0, nil
	}
	headroom, err := source.CompactionHeadroom(ctx)
	if err != nil {
		return 0, fmt.Errorf("measure compaction headroom: %w", err)
	}

	return headroom, nil
}

func (v *Vault) ShardGrowthHeadroom(ctx context.Context) (uint64, error) {
	lease, err := v.acquireEngineLease()
	if err != nil {
		return 0, err
	}
	defer lease.release()
	source, ok := lease.engine.(shardGrowthHeadroomSource)
	if !ok {
		return 0, nil
	}
	headroom, err := source.ShardGrowthHeadroom(ctx)
	if err != nil {
		return 0, fmt.Errorf("measure shard growth headroom: %w", err)
	}

	return headroom, nil
}
