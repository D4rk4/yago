package searchindex

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/index/scorch/mergeplan"
)

const (
	bleveReadSegmentConsolidationStartedMessage   = "bleve read segment consolidation started"
	bleveReadSegmentConsolidationCompletedMessage = "bleve read segment consolidation completed"
)

var forceBleveReadSegmentMerge = func(
	ctx context.Context,
	index *scorch.Scorch,
	options *mergeplan.MergePlanOptions,
) error {
	return index.ForceMerge(ctx, options)
}

type bleveReadSegmentState struct {
	documents uint64
	segments  uint64
}

type bleveReadSegmentShard interface {
	state() (bleveReadSegmentState, error)
	consolidate(context.Context) error
}

type scorchBleveReadSegmentShard struct {
	index bleve.Index
}

type bleveReadSegmentConsolidation struct {
	root      string
	shards    []bleveReadSegmentShard
	admission BleveRebuildGrowthAdmission
	measure   func(string) (uint64, bool, error)
}

func consolidateBleveReadSegments(
	ctx context.Context,
	root string,
	indexes []bleve.Index,
	admission BleveRebuildGrowthAdmission,
) error {
	shards := make([]bleveReadSegmentShard, len(indexes))
	for position, index := range indexes {
		shards[position] = scorchBleveReadSegmentShard{index: index}
	}
	consolidation := bleveReadSegmentConsolidation{
		root: root, shards: shards, admission: admission, measure: bleveRebuildFootprint,
	}

	return consolidation.run(ctx)
}

func (b *BleveDiskIndex) prepareBleveReads(
	ctx context.Context,
	root string,
	admission BleveRebuildGrowthAdmission,
) error {
	if err := consolidateBleveReadSegments(ctx, root, b.shards, admission); err != nil {
		return err
	}
	if err := loadBleveReadCache(ctx, b.shards); err != nil {
		return err
	}
	b.warm(ctx)

	return nil
}

func (c bleveReadSegmentConsolidation) run(ctx context.Context) error {
	states, before, required, err := c.inspect(ctx)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	if err := c.checkHeadroom(); err != nil {
		return err
	}
	slog.DebugContext(ctx, bleveReadSegmentConsolidationStartedMessage,
		slog.Int("shards", len(c.shards)),
		slog.Uint64("segments", before),
	)

	after := uint64(0)
	for position, shard := range c.shards {
		state, err := consolidateBleveReadSegmentShard(ctx, shard, states[position])
		if err != nil {
			return fmt.Errorf("consolidate bleve read shard %d: %w", position, err)
		}
		after += state.segments
	}
	slog.DebugContext(ctx, bleveReadSegmentConsolidationCompletedMessage,
		slog.Int("shards", len(c.shards)),
		slog.Uint64("segmentsBefore", before),
		slog.Uint64("segmentsAfter", after),
	)

	return nil
}

func (c bleveReadSegmentConsolidation) inspect(
	ctx context.Context,
) ([]bleveReadSegmentState, uint64, bool, error) {
	states := make([]bleveReadSegmentState, len(c.shards))
	total := uint64(0)
	required := false
	for position, shard := range c.shards {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, fmt.Errorf("inspect bleve read segments: %w", err)
		}
		state, err := shard.state()
		if err != nil {
			return nil, 0, false, fmt.Errorf(
				"inspect bleve read shard %d: %w",
				position,
				err,
			)
		}
		states[position] = state
		total += state.segments
		required = required || state.segments > desiredBleveReadSegments(state.documents)
	}

	return states, total, required, nil
}

func (c bleveReadSegmentConsolidation) checkHeadroom() error {
	if c.admission == nil {
		return nil
	}
	requiredBytes, measurementAvailable, err := c.measure(c.root)
	if err != nil {
		return fmt.Errorf("measure bleve read segment footprint: %w", err)
	}
	_, err = checkBleveGrowthAdmission(c.admission, requiredBytes, measurementAvailable)
	if err != nil {
		return fmt.Errorf("check bleve read segment storage headroom: %w", err)
	}

	return nil
}

func consolidateBleveReadSegmentShard(
	ctx context.Context,
	shard bleveReadSegmentShard,
	initial bleveReadSegmentState,
) (bleveReadSegmentState, error) {
	state := initial
	desired := desiredBleveReadSegments(initial.documents)
	acceptable := maximumBleveReadSegments(initial.documents)
	for state.segments > desired {
		if err := ctx.Err(); err != nil {
			return bleveReadSegmentState{}, fmt.Errorf("consolidation context: %w", err)
		}
		before := state.segments
		if err := shard.consolidate(ctx); err != nil {
			return bleveReadSegmentState{}, err
		}
		if err := ctx.Err(); err != nil {
			return bleveReadSegmentState{}, fmt.Errorf("consolidation context: %w", err)
		}
		var err error
		state, err = shard.state()
		if err != nil {
			return bleveReadSegmentState{}, err
		}
		if state.documents != initial.documents {
			return bleveReadSegmentState{}, fmt.Errorf(
				"document total changed from %d to %d",
				initial.documents,
				state.documents,
			)
		}
		if state.segments >= before {
			if state.segments <= acceptable {
				break
			}

			return bleveReadSegmentState{}, fmt.Errorf(
				"segment total did not fall from %d",
				before,
			)
		}
	}

	return state, nil
}

func desiredBleveReadSegments(documents uint64) uint64 {
	return bleveReadSegmentsForDocumentSpan(documents, diskMaxSegmentDocs)
}

func maximumBleveReadSegments(documents uint64) uint64 {
	return bleveReadSegmentsForDocumentSpan(documents, diskMaxSegmentDocs/2)
}

func bleveReadSegmentsForDocumentSpan(documents uint64, span uint64) uint64 {
	if documents == 0 {
		return 0
	}

	return 1 + (documents-1)/span
}

func (s scorchBleveReadSegmentShard) state() (bleveReadSegmentState, error) {
	documents, err := s.index.DocCount()
	if err != nil {
		return bleveReadSegmentState{}, fmt.Errorf("count documents: %w", err)
	}
	scorchIndex, err := bleveScorchImplementation(s.index)
	if err != nil {
		return bleveReadSegmentState{}, err
	}
	segments, ok := scorchIndex.StatsMap()["num_root_filesegments"].(uint64)
	if !ok {
		return bleveReadSegmentState{}, fmt.Errorf("root file segment statistic unavailable")
	}

	return bleveReadSegmentState{documents: documents, segments: segments}, nil
}

func (s scorchBleveReadSegmentShard) consolidate(ctx context.Context) error {
	scorchIndex, err := bleveScorchImplementation(s.index)
	if err != nil {
		return err
	}
	options := mergeplan.SingleSegmentMergePlanOptions
	options.MaxSegmentSize = diskMaxSegmentDocs + 1
	options.FloorSegmentSize = diskMaxSegmentDocs + 1
	if err := forceBleveReadSegmentMerge(ctx, scorchIndex, &options); err != nil {
		return fmt.Errorf("force merge: %w", err)
	}

	return nil
}

func bleveScorchImplementation(index bleve.Index) (*scorch.Scorch, error) {
	implementation, err := index.Advanced()
	if err != nil {
		return nil, fmt.Errorf("open advanced index: %w", err)
	}
	scorchIndex, ok := implementation.(*scorch.Scorch)
	if !ok {
		return nil, fmt.Errorf("advanced index is not scorch")
	}

	return scorchIndex, nil
}
