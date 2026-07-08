package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type indexSource struct {
	index searchindex.SearchIndex
	disk  indexDiskUsage
}

func newIndexSource(index searchindex.SearchIndex) indexSource {
	return indexSource{index: index}
}

func (s indexSource) Index(ctx context.Context) adminui.IndexStats {
	if s.index == nil {
		return adminui.IndexStats{}
	}

	stats, err := s.index.Stats(ctx)
	if err != nil {
		return adminui.IndexStats{}
	}

	view := adminui.IndexStats{
		Available: true,
		Documents: stats.Documents,
		Backend:   stats.Backend,
		UpdatedAt: formattedIndexStatsTime(stats.UpdatedAt),
	}
	s.disk.fill(ctx, &view)

	return view
}

// compactorSource adapts the runtime vault to the console's Compactor so the
// Index section's "Compact now" button can reclaim freed shard pages on demand.
type compactorSource struct {
	vault *vault.Vault
}

func newCompactorSource(v *vault.Vault) adminui.Compactor {
	if v == nil {
		return nil
	}

	return compactorSource{vault: v}
}

func (s compactorSource) Compact(ctx context.Context) (adminui.CompactionResult, error) {
	result, err := s.vault.Compact(ctx)
	if err != nil {
		return adminui.CompactionResult{}, fmt.Errorf("compact storage: %w", err)
	}

	return adminui.CompactionResult{
		ShardsCompacted: result.ShardsCompacted,
		BytesReclaimed:  humanBytes(result.BytesReclaimed),
	}, nil
}
