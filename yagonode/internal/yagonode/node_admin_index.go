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
	store     storageCompactor
	admission growthAdmission
}

type compactionAdmissionError struct {
	message string
}

func (e compactionAdmissionError) Error() string {
	return e.message
}

func (e compactionAdmissionError) CompactionOperatorMessage() string {
	return e.message
}

func newCompactorSource(
	v *vault.Vault,
	admissions ...growthAdmission,
) adminui.Compactor {
	if v == nil {
		return nil
	}
	source := compactorSource{store: v}
	if len(admissions) > 0 {
		source.admission = admissions[0]
	}

	return source
}

func (s compactorSource) Compact(ctx context.Context) (adminui.CompactionResult, error) {
	result := vault.CompactResult{}
	outcome, err := runStorageMaintenance(
		s.admission,
		func() (uint64, error) {
			return s.store.CompactionHeadroom(ctx)
		},
		func(required uint64) error {
			if required == 0 {
				return nil
			}
			var compactErr error
			result, compactErr = s.store.Compact(ctx)
			if compactErr != nil {
				return fmt.Errorf("compact storage: %w", compactErr)
			}

			return nil
		},
	)
	if err != nil && !outcome.Measured {
		return adminui.CompactionResult{}, fmt.Errorf("measure compaction headroom: %w", err)
	}
	if err != nil && !outcome.Started {
		return adminui.CompactionResult{}, compactionAdmissionError{message: fmt.Sprintf(
			"Compaction deferred: free filesystem space must exceed the configured reserve by %s. Free space or lower the reserve, then retry.",
			humanUnsignedBytes(outcome.RequiredBytes),
		)}
	}
	if err != nil {
		return adminui.CompactionResult{}, fmt.Errorf("compact storage: %w", err)
	}

	return adminui.CompactionResult{
		ShardsCompacted: result.ShardsCompacted,
		BytesReclaimed:  humanBytes(result.BytesReclaimed),
	}, nil
}
