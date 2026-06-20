package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

type RWIEvictionSweeper struct {
	evictor   ports.RWIEvictor
	policy    EvictionPolicy
	highWater int64
	lowWater  int64
	batch     int

	running         atomic.Bool
	sweeps          atomic.Int64
	urlsEvicted     atomic.Int64
	postingsEvicted atomic.Int64
}

func NewRWIEvictionSweeper(
	evictor ports.RWIEvictor,
	policy EvictionPolicy,
	highWater, lowWater int64,
	batch int,
) *RWIEvictionSweeper {
	return &RWIEvictionSweeper{
		evictor:   evictor,
		policy:    policy,
		highWater: highWater,
		lowWater:  lowWater,
		batch:     batch,
	}
}

func (s *RWIEvictionSweeper) Trigger() {
	if s.highWater <= 0 {
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		return
	}

	go func() {
		defer s.running.Store(false)
		if err := s.Sweep(context.Background()); err != nil {
			slog.Error("rwi eviction failed", "error", err)
		}
	}()
}

func (s *RWIEvictionSweeper) Sweep(ctx context.Context) error {
	if s.highWater <= 0 {
		return nil
	}

	used, err := s.evictor.UsedBytes(ctx)
	if err != nil {
		return fmt.Errorf("measure used bytes: %w", err)
	}
	if used < s.highWater {
		return nil
	}

	s.sweeps.Add(1)
	for used > s.lowWater {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sweep cancelled: %w", err)
		}

		candidates, err := s.evictor.SelectEvictionCandidates(ctx, s.batch)
		if err != nil {
			return fmt.Errorf("select candidates: %w", err)
		}
		if len(candidates) == 0 {
			break
		}

		result, err := s.policy.Evict(ctx, candidates)
		if err != nil {
			return fmt.Errorf("evict candidates: %w", err)
		}
		s.urlsEvicted.Add(int64(result.URLsDeleted))
		s.postingsEvicted.Add(int64(result.PostingsDeleted))
		slog.Debug(
			"rwi eviction progress",
			"urls", result.URLsDeleted,
			"postings", result.PostingsDeleted,
		)
		if result.URLsDeleted == 0 && result.PostingsDeleted == 0 {
			break
		}

		used, err = s.evictor.UsedBytes(ctx)
		if err != nil {
			return fmt.Errorf("measure used bytes: %w", err)
		}
	}

	return nil
}

func (s *RWIEvictionSweeper) Sweeps() int64          { return s.sweeps.Load() }
func (s *RWIEvictionSweeper) URLsEvicted() int64     { return s.urlsEvicted.Load() }
func (s *RWIEvictionSweeper) PostingsEvicted() int64 { return s.postingsEvicted.Load() }
